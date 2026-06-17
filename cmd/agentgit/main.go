package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/hooks"
	"github.com/minkuik/agentgit/internal/tui"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "agentgit:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return cmdBrowse(nil)
	}
	switch args[0] {
	case "setup":
		return cmdSetup(args[1:])
	case "hook":
		return cmdHook(args[1:])
	case "-h", "--help", "help":
		usage()
		return nil
	case "version", "--version":
		fmt.Println("agentgit", version)
		return nil
	default:
		return cmdBrowse(args)
	}
}

func usage() {
	fmt.Println(`usage:
  agentgit [--limit 500] [path]
  agentgit setup codex|gemini|claude
  agentgit hook codex|gemini|claude|post-commit
  agentgit version

commands:
  agentgit                             browse request-linked commits for the current path
  agentgit <path>                      browse request-linked commits for a path
  setup codex|gemini|claude            install lifecycle hooks once for this PC
  hook codex|gemini|claude|post-commit internal hook entrypoint
  version                              print version`)
}

func cmdBrowse(args []string) error {
	fs := flag.NewFlagSet("agentgit", flag.ContinueOnError)
	limit := fs.Int("limit", 500, "maximum commits to show")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	path := "."
	if fs.NArg() > 1 {
		return errors.New("usage: agentgit [--limit 500] [path]")
	}
	if fs.NArg() == 1 {
		path = fs.Arg(0)
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		path = filepath.Dir(path)
	}
	root, err := git.RepoRoot(path)
	if err != nil {
		return err
	}
	return tui.Run(root, *limit)
}

func cmdSetup(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: agentgit setup codex|gemini|claude")
	}
	switch args[0] {
	case "codex":
		hooksPath, err := hooks.InstallCodex()
		if err != nil {
			return err
		}
		fmt.Println("installed codex hooks:", hooksPath)
		fmt.Println("codex may ask you to review and trust the new hooks via /hooks")
	case "gemini":
		hooksPath, err := hooks.InstallGemini()
		if err != nil {
			return err
		}
		fmt.Println("installed gemini hooks:", hooksPath)
	case "claude":
		hooksPath, err := hooks.InstallClaude()
		if err != nil {
			return err
		}
		fmt.Println("installed claude hooks:", hooksPath)
		fmt.Println("review the installed hooks in Claude Code with /hooks")
	default:
		return errors.New("usage: agentgit setup codex|gemini|claude")
	}
	return nil
}

func cmdHook(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: agentgit hook codex|gemini|claude|post-commit")
	}
	switch args[0] {
	case "codex":
		if err := hooks.HandleCodex(os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, "agentgit hook codex:", err)
		}
		return nil
	case "gemini":
		if err := hooks.HandleGemini(os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, "agentgit hook gemini:", err)
		}
		return nil
	case "claude":
		if err := hooks.HandleClaude(os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, "agentgit hook claude:", err)
		}
		return nil
	case "post-commit":
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if err := hooks.HandlePostCommit(cwd); err != nil {
			fmt.Fprintln(os.Stderr, "agentgit hook post-commit:", err)
		}
		return nil
	default:
		return errors.New("usage: agentgit hook codex|gemini|claude|post-commit")
	}
}
