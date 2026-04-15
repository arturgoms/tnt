package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/arturgoms/tnt/internal/recents"
	"github.com/arturgoms/tnt/internal/session"
	"github.com/arturgoms/tnt/internal/tmux"
	"github.com/spf13/cobra"
)

var sessionKillCmd = &cobra.Command{
	Use:   "kill [session]",
	Short: "Save and kill a tmux session (switches to last if killing current)",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runSessionKill(args)
	},
}

func runSessionKill(args []string) {
	cfg := app.Config

	current, err := tmux.SessionName()
	if err != nil {
		fmt.Fprintln(os.Stderr, "not in a tmux session")
		os.Exit(1)
	}

	target := current
	if len(args) > 0 {
		target = args[0]
	}

	if !tmux.HasSession(target) {
		fmt.Fprintf(os.Stderr, "session %q not found\n", target)
		os.Exit(1)
	}

	if target == current {
		recentList := recents.Load(cfg.Paths.State)
		next := ""
		sessions, _ := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
		activeSet := map[string]bool{}
		for _, s := range strings.Split(strings.TrimSpace(string(sessions)), "\n") {
			if s != "" && s != target {
				activeSet[s] = true
			}
		}

		for _, name := range recentList.Repos {
			if activeSet[name] {
				next = name
				break
			}
		}
		if next == "" {
			for name := range activeSet {
				next = name
				break
			}
		}

		if next != "" {
			exec.Command("tmux", "switch-client", "-t", next).Run()
		}
	}

	if cfg.Session.SaveRestore {
		session.Save(cfg, target)
	}
	exec.Command("tmux", "kill-session", "-t", target).Run()
}
