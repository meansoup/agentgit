package terminal

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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
	ctrlC = byte(0x03)
	ctrlG = byte(0x07)
	esc   = byte(0x1b)
)

type Config struct {
	Root    string
	Command []string
	Limit   int
}

type session struct {
	root      string
	command   []string
	limit     int
	ptmx      *os.File
	process   *os.Process
	mu        sync.Mutex
	cond      *sync.Cond
	width     int
	height    int
	prefix    bool
	help      bool
	paused    bool
	status    string
	prefixSeq []byte
	actionCh  chan actionRequest
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

	s.drawStatus("ready")

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	done := make(chan struct{})
	defer close(done)
	go s.copyPTY(done)
	go s.copyInput(done)
	go s.watchResize(done)

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
	for _, candidate := range []string{"codex", "claude", "gemini"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return []string{candidate}, nil
		}
	}
	return nil, errors.New("no agent command provided and no codex, claude, or gemini executable was found in PATH")
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
		if s.prefixActive() {
			if start < i {
				_, _ = s.ptmx.Write(data[start:i])
			}
			s.handlePrefixKey(b)
			start = i + 1
			continue
		}
		if b == ctrlG {
			if start < i {
				_, _ = s.ptmx.Write(data[start:i])
			}
			s.setPrefix(true)
			s.drawStatus("prefix")
			start = i + 1
		}
	}
	if start < len(data) {
		_, _ = s.ptmx.Write(data[start:])
	}
}

func (s *session) handlePrefixKey(key byte) {
	if key == esc {
		s.prefixSeq = append(s.prefixSeq[:0], key)
		return
	}
	if len(s.prefixSeq) > 0 {
		s.prefixSeq = append(s.prefixSeq, key)
		decoded, ok, complete := decodePrefixSequence(s.prefixSeq)
		if !complete {
			return
		}
		sequence := append([]byte(nil), s.prefixSeq...)
		s.prefixSeq = nil
		if ok {
			s.handlePrefixKey(decoded)
			return
		}
		s.setPrefix(false)
		s.drawStatus(fmt.Sprintf("unknown prefix key %s", printableSequence(sequence)))
		return
	}
	s.setPrefix(false)
	switch key {
	case '?', 'h':
		s.toggleHelp()
		s.drawStatus("")
	case 'c':
		s.setPaused(true)
		s.drawStatus("opening commits...")
		done := make(chan struct{})
		s.actionCh <- actionRequest{action: actionCommitView, done: done}
		<-done
	case 'r':
		s.drawStatus("redrawn")
	case 'g', ctrlG:
		_, _ = s.ptmx.Write([]byte{ctrlG})
		s.drawStatus("sent Ctrl-G")
	case 'q':
		s.drawStatus("terminating...")
		go s.terminateProcess()
	case ctrlC:
		s.drawStatus("prefix canceled")
	default:
		s.drawStatus(fmt.Sprintf("unknown prefix key %s", printableKey(key)))
	}
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
		if returnErr == nil && rawErr != nil {
			returnErr = rawErr
		}
	}()

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, "browse", "--limit", strconv.Itoa(s.limit), s.root)
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
	mode := "terminal"
	if s.prefix {
		mode = "prefix"
	}
	command := strings.Join(s.command, " ")
	cwd := filepath.Base(s.root)
	if cwd == "." || cwd == string(filepath.Separator) {
		cwd = s.root
	}
	left := fmt.Sprintf(" agentgit:%s  %s  %s ", mode, emptyFallback(cwd, "."), emptyFallback(command, "agent"))
	right := " Ctrl-G h help "
	if s.help || s.prefix {
		right = " Ctrl-G: c commits | q quit | r redraw | g send Ctrl-G | h help "
	}
	if s.status != "" {
		right = " " + s.status + " |" + right
	}
	return reverse(padStatus(left, right, s.width))
}

func padStatus(left, right string, width int) string {
	if width <= 0 {
		return left + right
	}
	left = truncateRunes(left, width)
	space := width - runeWidth(left) - runeWidth(right)
	if space < 1 {
		right = truncateRunes(right, max(0, width-runeWidth(left)-1))
		space = width - runeWidth(left) - runeWidth(right)
	}
	if space < 0 {
		space = 0
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

func printableSequence(sequence []byte) string {
	if len(sequence) == 0 {
		return ""
	}
	parts := make([]string, 0, len(sequence))
	for _, key := range sequence {
		parts = append(parts, printableKey(key))
	}
	return strings.Join(parts, " ")
}

func decodePrefixSequence(sequence []byte) (byte, bool, bool) {
	if len(sequence) == 0 || sequence[0] != esc {
		return 0, false, true
	}
	if len(sequence) == 1 {
		return 0, false, false
	}
	if sequence[1] != '[' {
		if sequence[1] == 'O' && len(sequence) < 3 {
			return 0, false, false
		}
		return 0, false, true
	}
	if len(sequence) == 2 {
		return 0, false, false
	}
	final := sequence[len(sequence)-1]
	if final < 0x40 || final > 0x7e {
		return 0, false, false
	}
	if final == 'u' {
		if key, ok := decodeCSIU(sequence[2 : len(sequence)-1]); ok {
			return key, true, true
		}
	}
	if final == '~' {
		if key, ok := decodeModifyOtherKeys(sequence[2 : len(sequence)-1]); ok {
			return key, true, true
		}
	}
	return 0, false, true
}

func decodeCSIU(body []byte) (byte, bool) {
	fields := splitCSIFields(body)
	if len(fields) == 0 {
		return 0, false
	}
	code, ok := atoiBytes(fields[0])
	if !ok {
		return 0, false
	}
	modifier := 1
	if len(fields) > 1 {
		if parsed, ok := atoiBytes(fields[1]); ok {
			modifier = parsed
		}
	}
	return decodedQuestionMark(code, modifier)
}

func decodeModifyOtherKeys(body []byte) (byte, bool) {
	fields := splitCSIFields(body)
	if len(fields) < 3 {
		return 0, false
	}
	prefix, ok := atoiBytes(fields[0])
	if !ok || prefix != 27 {
		return 0, false
	}
	modifier, ok := atoiBytes(fields[1])
	if !ok {
		return 0, false
	}
	code, ok := atoiBytes(fields[2])
	if !ok {
		return 0, false
	}
	return decodedQuestionMark(code, modifier)
}

func decodedQuestionMark(code int, modifier int) (byte, bool) {
	if code == int('?') || code == int('/') && hasShiftModifier(modifier) {
		return '?', true
	}
	return 0, false
}

func hasShiftModifier(modifier int) bool {
	return modifier > 1 && (modifier-1)&1 != 0
}

func splitCSIFields(body []byte) [][]byte {
	return bytes.FieldsFunc(body, func(r rune) bool {
		return r == ';' || r == ':'
	})
}

func atoiBytes(value []byte) (int, bool) {
	if len(value) == 0 {
		return 0, false
	}
	out := 0
	for _, b := range value {
		if b < '0' || b > '9' {
			return 0, false
		}
		out = out*10 + int(b-'0')
	}
	return out, true
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

func (s *session) prefixActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prefix
}

func (s *session) setPrefix(active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prefix = active
	if !active {
		s.prefixSeq = nil
	}
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

func (s *session) toggleHelp() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.help = !s.help
}
