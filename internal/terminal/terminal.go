package terminal

import (
	"os"
	"os/exec"
	"strings"
)

type Config struct {
	Root    string
	Command []string
}

// Run starts the command with the caller's terminal connected directly.
//
// The terminal emulator, rather than agentgit, owns the screen, cursor,
// scrollback, input modes, and window size. This is important for interactive
// programs such as shells and coding agents: inserting a PTY relay or a
// status line here can change how those programs handle Enter and redraws.
func Run(config Config) error {
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
