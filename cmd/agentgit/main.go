package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/hooks"
	"github.com/minkuik/agentgit/internal/state"
	"github.com/minkuik/agentgit/internal/store"
	"github.com/minkuik/agentgit/internal/tui"
)

var providers = map[string]bool{
	"codex":  true,
	"gemini": true,
	"claude": true,
}

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "agentgit:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("command is required")
	}
	switch args[0] {
	case "setup":
		return cmdSetup()
	case "setup-local":
		return cmdSetupLocal()
	case "log":
		return cmdLog(args[1:])
	case "hook":
		return cmdHook(args[1:])
	case "request":
		return cmdRequest(args[1:])
	case "codex", "gemini", "claude":
		return cmdProvider(args[0], args[1:])
	case "-h", "--help", "help":
		usage()
		return nil
	case "version", "--version":
		fmt.Println("agentgit", version)
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Println(`usage: agentgit <command> [options]

commands:
  setup                         install global hooks once for this PC
  setup-local                   install hooks only in the current repository
  log [--limit 500]             browse commits with linked AI requests
  codex start|commit|finish     record and commit Codex requests
  gemini start|commit|finish    record and commit Gemini requests
  claude start|commit|finish    record and commit Claude requests
  request --provider ...        generic provider command
  hook post-commit              internal Git hook entrypoint
  version                       print version`)
}

func cmdSetup() error {
	dbPath, err := store.Init()
	if err != nil {
		return err
	}
	hook, err := hooks.InstallGlobal()
	if err != nil {
		return err
	}
	fmt.Println("initialized db:", dbPath)
	fmt.Println("installed global hook:", hook)
	fmt.Println("configured: git config --global core.hooksPath", filepath.Dir(hook))
	return nil
}

func cmdSetupLocal() error {
	root, err := git.RepoRoot("")
	if err != nil {
		return err
	}
	dbPath, err := store.Init()
	if err != nil {
		return err
	}
	hook, err := hooks.InstallLocal(root)
	if err != nil {
		return err
	}
	fmt.Println("initialized db:", dbPath)
	fmt.Println("installed hook:", hook)
	return nil
}

func cmdLog(args []string) error {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	limit := fs.Int("limit", 500, "maximum commits to show")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	root, err := git.RepoRoot("")
	if err != nil {
		return err
	}
	if _, err := store.Init(); err != nil {
		return err
	}
	return tui.Run(root, *limit)
}

func cmdHook(args []string) error {
	if len(args) != 1 || args[0] != "post-commit" {
		return errors.New("usage: agentgit hook post-commit")
	}
	return hooks.HandlePostCommit()
}

func cmdRequest(args []string) error {
	fs := flag.NewFlagSet("request", flag.ContinueOnError)
	provider := fs.String("provider", "", "provider name")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *provider == "" || !providers[*provider] {
		return errors.New("--provider must be one of codex, gemini, claude")
	}
	return cmdProvider(*provider, fs.Args())
}

func cmdProvider(provider string, args []string) error {
	if len(args) == 0 {
		return errors.New("provider subcommand is required")
	}
	switch args[0] {
	case "start":
		return cmdStart(provider, args[1:])
	case "commit":
		return cmdCommit(args[1:])
	case "finish":
		return cmdFinish()
	default:
		return fmt.Errorf("unknown provider subcommand %q", args[0])
	}
}

func cmdStart(provider string, args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	providerFlag := fs.String("provider", provider, "provider name")
	model := fs.String("model", "", "model name")
	message := fs.String("message", "", "request message")
	includeCurrent := fs.Bool("include-current", false, "treat existing dirty files as part of this request")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *providerFlag == "" || !providers[*providerFlag] {
		return errors.New("provider must be one of codex, gemini, claude")
	}
	if *model == "" {
		return errors.New("--model is required")
	}
	if *message == "" {
		return errors.New("--message is required")
	}
	root, err := git.RepoRoot("")
	if err != nil {
		return err
	}
	baseline := map[string]bool{}
	if !*includeCurrent {
		baseline, err = git.StatusPaths(root)
		if err != nil {
			return err
		}
	}
	id, err := store.CreateRequest(*providerFlag, *model, *message, root, baseline)
	if err != nil {
		return err
	}
	if err := state.SetActiveRequest(root, id); err != nil {
		return err
	}
	fmt.Printf("started %s request #%d\n", *providerFlag, id)
	return nil
}

func cmdCommit(args []string) error {
	fs := flag.NewFlagSet("commit", flag.ContinueOnError)
	message := fs.String("m", "", "commit message")
	messageLong := fs.String("message", "", "commit message")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	commitMessage := *message
	if commitMessage == "" {
		commitMessage = *messageLong
	}
	if commitMessage == "" {
		return errors.New("-m or --message is required")
	}
	root, err := git.RepoRoot("")
	if err != nil {
		return err
	}
	requestID, ok, err := state.GetActiveRequest(root)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no active agentgit request")
	}
	req, err := store.GetRequest(requestID)
	if err != nil {
		return err
	}
	reqRoot, err := filepath.Abs(req.RepoRoot)
	if err != nil {
		return err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if reqRoot != rootAbs {
		return fmt.Errorf("active request #%d belongs to %s", requestID, req.RepoRoot)
	}
	current, err := git.StatusPaths(root)
	if err != nil {
		return err
	}
	owned := map[string]bool{}
	for path := range current {
		if !req.BaselinePaths[path] {
			owned[path] = true
		}
	}
	commitHash, err := git.CommitPaths(root, owned, commitMessage)
	if err != nil {
		return err
	}
	if err := store.LinkCommit(requestID, commitHash, root); err != nil {
		return err
	}
	fmt.Printf("committed %s for %s request #%d\n", shortHash(commitHash), req.Provider, req.ID)
	return nil
}

func cmdFinish() error {
	root, err := git.RepoRoot("")
	if err != nil {
		return err
	}
	requestID, ok, err := state.GetActiveRequest(root)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no active agentgit request")
	}
	if err := store.FinishRequest(requestID); err != nil {
		return err
	}
	if err := state.ClearActiveRequest(root); err != nil {
		return err
	}
	fmt.Printf("finished request #%d\n", requestID)
	return nil
}

func shortHash(hash string) string {
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}
