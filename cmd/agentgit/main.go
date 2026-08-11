package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/terminal"
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
		return cmdTerminal(args[1:])
	}
	if len(args) == 0 {
		return cmdTerminal(nil)
	}
	switch args[0] {
	case "-h", "--help", "help":
		usage()
		return nil
	case "version", "--version":
		fmt.Println("agentgit", version)
		return nil
	case "browse", "commits":
		return cmdBrowse(args[1:])
	case "terminal", "term":
		return cmdTerminal(args[1:])
	default:
		return cmdTerminal(args)
	}
}

func usage() {
	fmt.Println(`usage:
  agentgit
  agentgit [--] [agent-command ...]
  agentgit browse [--limit 500] [path]
  agentgit terminal [--] [agent-command ...]
  agentgit version

commands:
  agentgit                             run an agent CLI inside agentgit's terminal wrapper
  agentgit -- codex                    run a specific agent command inside the wrapper
  browse                               open the Git history and transcript browser directly
  terminal                             alias for the default terminal wrapper
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

func cmdTerminal(args []string) error {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	root, err := git.RepoRoot(".")
	if err != nil {
		return err
	}
	return terminal.Run(terminal.Config{
		Root:    root,
		Command: args,
	})
}
