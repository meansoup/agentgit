package terminal

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
	"golang.org/x/term"
)

const (
	ctrlG = byte(0x07)
	ctrlL = byte(0x0c)
)

type Config struct {
	Root    string
	Command []string
	Limit   int
}

type session struct {
	root        string
	command     []string
	limit       int
	ptmx        *os.File
	process     *os.Process
	mu          sync.Mutex
	cond        *sync.Cond
	width       int
	height      int
	paused      bool
	status      string
	statusUntil time.Time
	gitState    gitStatusState
	agentAlt    bool
	ptyTail     []byte
	actionCh    chan actionRequest
}

type action int

const (
	actionCommitView action = iota + 1
)

type actionRequest struct {
	action action
	done   chan struct{}
}

func Run(config Config) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return runPlain(config)
	}
	command, err := resolveCommand(config.Command)
	if err != nil {
		return err
	}
	root := config.Root
	if root == "" {
		root = "."
	}
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return err
	}
	if height < 2 {
		return errors.New("terminal mode requires at least two rows")
	}
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer func() {
		_ = term.Restore(int(os.Stdin.Fd()), oldState)
	}()

	s := &session{
		root:     root,
		command:  command,
		limit:    defaultLimit(config.Limit),
		width:    width,
		height:   height,
		gitState: loadGitStatus(root),
		actionCh: make(chan actionRequest),
	}
	s.cond = sync.NewCond(&s.mu)
	s.enterScreen(true)
	defer s.leaveScreen()

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = root
	cmd.Env = os.Environ()
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(max(1, height-1)),
		Cols: uint16(max(1, width)),
	})
	if err != nil {
		return err
	}
	defer ptmx.Close()
	s.ptmx = ptmx
	s.process = cmd.Process

	s.drawStatus("")

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	done := make(chan struct{})
	defer close(done)
	go s.copyPTY(done)
	go s.copyInput(done)
	go s.watchResize(done)
	go s.watchGitStatus(done)

	for {
		select {
		case err = <-waitCh:
			s.drawStatus("process exited")
			time.Sleep(80 * time.Millisecond)
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					return fmt.Errorf("%s exited: %w", command[0], err)
				}
				return err
			}
			return nil
		case request := <-s.actionCh:
			switch request.action {
			case actionCommitView:
				if err := s.openCommitView(&oldState); err != nil {
					s.drawStatus("commit view failed: " + err.Error())
				} else {
					s.drawStatus("returned from commits")
				}
				s.setPaused(false)
			}
			close(request.done)
		}
	}
}

func runPlain(config Config) error {
	command, err := resolveCommand(config.Command)
	if err != nil {
		return err
	}
	cmd := exec.Command(command[0], command[1:]...)
	if config.Root != "" {
		cmd.Dir = config.Root
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func resolveCommand(command []string) ([]string, error) {
	if len(command) > 0 {
		return command, nil
	}
	if configured := strings.TrimSpace(os.Getenv("AGENTGIT_AGENT")); configured != "" {
		return []string{shellPath(), "-lc", configured}, nil
	}
	return []string{shellPath()}, nil
}

func shellPath() string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return shell
	}
	return "/bin/sh"
}

func (s *session) enterScreen(clear bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprint(os.Stdout, "\x1b[?25h")
	if clear {
		fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H")
	}
	s.setScrollRegionLocked()
}

func (s *session) leaveScreen() {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(os.Stdout, "\x1b[r\x1b[%d;1H\x1b[2K\x1b[?25h", max(1, s.height))
}

func (s *session) copyPTY(done <-chan struct{}) {
	buf := make([]byte, 32*1024)
	for {
		if s.waitIfPaused(done) {
			return
		}
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			if s.waitIfPaused(done) {
				return
			}
			s.mu.Lock()
			s.observePTYOutputLocked(buf[:n])
			_, _ = os.Stdout.Write(buf[:n])
			s.drawStatusLocked("")
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (s *session) copyInput(done <-chan struct{}) {
	buf := make([]byte, 4096)
	for {
		if s.waitIfPaused(done) {
			return
		}
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			s.handleInput(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (s *session) handleInput(data []byte) {
	start := 0
	for i, b := range data {
		if b == ctrlG {
			if start < i {
				_, _ = s.ptmx.Write(data[start:i])
			}
			s.openCommitViewFromInput()
			start = i + 1
		}
	}
	if start < len(data) {
		_, _ = s.ptmx.Write(data[start:])
	}
}

func (s *session) openCommitViewFromInput() {
	s.setPaused(true)
	s.drawStatus("opening commits...")
	done := make(chan struct{})
	s.actionCh <- actionRequest{action: actionCommitView, done: done}
	<-done
}

func (s *session) terminateProcess() {
	if s.process == nil {
		return
	}
	_ = s.signalAgent(syscall.SIGTERM)
	time.Sleep(700 * time.Millisecond)
	_ = s.signalAgent(syscall.SIGKILL)
}

func (s *session) signalAgent(signal syscall.Signal) error {
	if s.process == nil {
		return nil
	}
	pid := s.process.Pid
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, signal); err == nil {
		return nil
	}
	return s.process.Signal(signal)
}

func (s *session) openCommitView(oldState **term.State) (returnErr error) {
	agentAlt := s.agentAltScreen()
	s.leaveScreen()
	if *oldState != nil {
		_ = term.Restore(int(os.Stdin.Fd()), *oldState)
	}
	defer func() {
		newState, rawErr := term.MakeRaw(int(os.Stdin.Fd()))
		if rawErr == nil {
			*oldState = newState
		}
		s.enterScreen(false)
		s.resizePTY()
		s.resumeAgentDisplay(agentAlt)
		if returnErr == nil && rawErr != nil {
			returnErr = rawErr
		}
	}()

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, "browse", "--limit", strconv.Itoa(s.limit), s.root)
	cmd.Env = append(os.Environ(), "AGENTGIT_EMBEDDED_BROWSER=1")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	signal.Ignore(os.Interrupt)
	defer signal.Reset(os.Interrupt)
	return cmd.Run()
}

func (s *session) resumeAgentDisplay(agentAlt bool) {
	if !agentAlt {
		return
	}
	s.mu.Lock()
	fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25h")
	s.setScrollRegionLocked()
	s.mu.Unlock()
	_ = s.signalAgent(syscall.SIGWINCH)
	if s.ptmx != nil {
		_, _ = s.ptmx.Write([]byte{ctrlL})
	}
}

func (s *session) watchResize(done <-chan struct{}) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	defer signal.Stop(ch)
	for {
		select {
		case <-done:
			return
		case <-ch:
			s.resize()
		}
	}
}

func (s *session) resize() {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 || height <= 1 {
		return
	}
	s.mu.Lock()
	s.width = width
	s.height = height
	s.drawStatusLocked("resized")
	s.mu.Unlock()
	s.resizePTY()
}

func (s *session) watchGitStatus(done <-chan struct{}) {
	s.refreshGitStatus()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			s.refreshGitStatus()
		}
	}
}

func (s *session) refreshGitStatus() {
	state := loadGitStatus(s.root)
	s.mu.Lock()
	s.gitState = state
	s.drawStatusLocked("")
	s.mu.Unlock()
}

func (s *session) resizePTY() {
	if s.ptmx == nil {
		return
	}
	s.mu.Lock()
	width := s.width
	height := s.height
	s.mu.Unlock()
	_ = pty.Setsize(s.ptmx, &pty.Winsize{
		Rows: uint16(max(1, height-1)),
		Cols: uint16(max(1, width)),
	})
}

func (s *session) drawStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drawStatusLocked(status)
}

func (s *session) drawStatusLocked(status string) {
	if status != "" {
		s.status = status
		s.statusUntil = time.Now().Add(3 * time.Second)
	}
	if s.height <= 0 {
		return
	}
	line := s.statusLine()
	fmt.Fprint(os.Stdout, "\x1b7")
	s.setScrollRegionLocked()
	fmt.Fprintf(os.Stdout, "\x1b[%d;1H\x1b[2K%s\x1b8", s.height, line)
}

func (s *session) setScrollRegionLocked() {
	if s.height > 1 {
		fmt.Fprintf(os.Stdout, "\x1b[1;%dr", s.height-1)
	}
}

func (s *session) statusLine() string {
	left := " agentgit "
	right := " " + s.gitState.String() + " | Ctrl-G commits "
	if s.status != "" && time.Now().Before(s.statusUntil) {
		right = " " + s.status + " |" + right
	}
	return reverse(padStatus(left, right, s.width))
}

func padStatus(left, right string, width int) string {
	if width <= 0 {
		return left + right
	}
	rightWidth := runeWidth(right)
	if rightWidth >= width {
		return truncateRunes(right, width)
	}
	left = truncateRunes(left, max(0, width-rightWidth-1))
	space := width - runeWidth(left) - runeWidth(right)
	if space < 1 {
		space = 1
	}
	return left + strings.Repeat(" ", space) + right
}

func truncateRunes(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if runeWidth(value) <= width {
		return value
	}
	out := make([]rune, 0, width)
	for _, r := range value {
		if len(out) >= width {
			break
		}
		out = append(out, r)
	}
	return string(out)
}

func reverse(value string) string {
	return "\x1b[7m" + value + "\x1b[0m"
}

func printableKey(key byte) string {
	if key < 0x20 {
		return fmt.Sprintf("Ctrl-%c", key+'@')
	}
	if key == 0x7f {
		return "Backspace"
	}
	if utf8.Valid([]byte{key}) {
		return string(key)
	}
	return fmt.Sprintf("0x%02x", key)
}

func (s *session) observePTYOutputLocked(data []byte) {
	if len(data) == 0 {
		return
	}
	combined := make([]byte, 0, len(s.ptyTail)+len(data))
	combined = append(combined, s.ptyTail...)
	combined = append(combined, data...)
	for {
		index := bytes.Index(combined, []byte("\x1b[?"))
		if index < 0 {
			break
		}
		combined = combined[index+3:]
		end := bytes.IndexAny(combined, "hl")
		if end < 0 {
			break
		}
		mode := string(combined[:end])
		final := combined[end]
		if mode == "47" || mode == "1047" || mode == "1049" {
			s.agentAlt = final == 'h'
		}
		combined = combined[end+1:]
	}
	if len(data) > 32 {
		s.ptyTail = append(s.ptyTail[:0], data[len(data)-32:]...)
		return
	}
	tail := append(append([]byte(nil), s.ptyTail...), data...)
	if len(tail) > 32 {
		tail = tail[len(tail)-32:]
	}
	s.ptyTail = tail
}

func (s *session) agentAltScreen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentAlt
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func runeWidth(value string) int {
	return len([]rune(value))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func defaultLimit(limit int) int {
	if limit > 0 {
		return limit
	}
	return 500
}

func (s *session) waitIfPaused(done <-chan struct{}) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.paused {
		select {
		case <-done:
			return true
		default:
		}
		s.cond.Wait()
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func (s *session) setPaused(paused bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = paused
	if !paused {
		s.cond.Broadcast()
	}
}
