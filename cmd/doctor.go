package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/arturgoms/tnt/internal/config"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system dependencies and configuration",
	Run: func(cmd *cobra.Command, args []string) {
		runDoctor()
	},
}

func runDoctor() {
	cfg := app.Config

	green := lipgloss.Color("#AAD94C")
	red := lipgloss.Color("#D95757")
	yellow := lipgloss.Color("#E6B450")
	gray := lipgloss.Color("#555E73")
	blue := lipgloss.Color("#39BAE6")

	tick := lipgloss.NewStyle().Foreground(green).Render("✓")
	cross := lipgloss.NewStyle().Foreground(red).Render("✗")
	bang := lipgloss.NewStyle().Foreground(yellow).Render("!")
	dot := lipgloss.NewStyle().Foreground(gray).Render("·")

	section := func(name string) {
		fmt.Println()
		fmt.Println("  " + lipgloss.NewStyle().Bold(true).Foreground(blue).Render(name))
	}

	line := func(icon, label, detail string) {
		fmt.Printf("  %s  %-18s %s\n", icon, label,
			lipgloss.NewStyle().Foreground(gray).Render(detail))
	}

	binary := func(name string) (string, bool) {
		path, err := exec.LookPath(name)
		return path, err == nil
	}

	section("requirements")

	tmuxOK := false
	if out, err := exec.Command("tmux", "-V").Output(); err == nil {
		v := strings.TrimSpace(strings.TrimPrefix(string(out), "tmux "))
		parts := strings.SplitN(v, ".", 2)
		major, _ := strconv.Atoi(parts[0])
		minor := 0
		if len(parts) == 2 {
			minor, _ = strconv.Atoi(strings.TrimRight(parts[1], "abcdefghijklmnopqrstuvwxyz-"))
		}
		if major > 3 || (major == 3 && minor >= 2) {
			line(tick, "tmux", v)
			tmuxOK = true
		} else {
			line(cross, "tmux", v+" — 3.2+ required")
		}
	} else {
		line(cross, "tmux", "not found — install tmux 3.2+")
	}

	if out, err := exec.Command("git", "--version").Output(); err == nil {
		v := strings.TrimSpace(strings.TrimPrefix(string(out), "git version "))
		line(tick, "git", v)
	} else {
		line(cross, "git", "not found")
	}

	section("tmux config")

	if !tmuxOK {
		line(dot, "skipped", "tmux not available")
	} else {
		for _, check := range []struct{ key, want, hint string }{
			{"automatic-rename", "off", "add to tmux.conf: setw -g automatic-rename off"},
			{"allow-rename", "off", "add to tmux.conf: set -g allow-rename off"},
		} {
			out, err := exec.Command("tmux", "show-option", "-gv", check.key).Output()
			if err != nil {
				line(bang, check.key, "tmux server not running — start tmux and recheck")
				continue
			}
			val := strings.TrimSpace(string(out))
			if val == check.want {
				line(tick, check.key, val)
			} else {
				line(cross, check.key, fmt.Sprintf("%q — %s", val, check.hint))
			}
		}
	}

	section("config")

	if _, err := os.Stat(config.DefaultConfigPath); err == nil {
		line(tick, "config.toml", config.DefaultConfigPath)
	} else {
		line(cross, "config.toml", "not found — run: tnt install")
	}

	for _, d := range []struct{ name, path string }{
		{"state", cfg.Paths.State},
		{"layouts", cfg.Paths.Layouts},
		{"projects", cfg.Paths.Projects},
		{"scripts", cfg.Paths.Scripts},
	} {
		if _, err := os.Stat(d.path); err == nil {
			line(tick, d.name, d.path)
		} else {
			line(cross, d.name, d.path+" — run: tnt install")
		}
	}

	section("integrations")

	home, _ := os.UserHomeDir()

	_, opencodeFound := binary("opencode")
	switch {
	case cfg.Integrations.Opencode && opencodeFound:
		line(tick, "opencode", "enabled · found")
	case cfg.Integrations.Opencode && !opencodeFound:
		line(cross, "opencode", "enabled in config but opencode not found in PATH")
	case !cfg.Integrations.Opencode && opencodeFound:
		line(bang, "opencode", "available but disabled — set integrations.opencode = true")
	default:
		line(dot, "opencode", "disabled")
	}

	if cfg.Integrations.Opencode {
		if _, found := binary("sqlite3"); found {
			line(tick, "sqlite3", "found (opencode session detection)")
		} else {
			line(bang, "sqlite3", "not found — opencode session detection unavailable")
		}
	}

	_, ghFound := binary("gh")
	switch {
	case cfg.Integrations.GitHub && ghFound:
		if exec.Command("gh", "auth", "status").Run() == nil {
			line(tick, "github", "enabled · gh found · authenticated")
		} else {
			line(bang, "github", "enabled · gh found · not authenticated — run: gh auth login")
		}
	case cfg.Integrations.GitHub && !ghFound:
		line(cross, "github", "enabled in config but gh not found in PATH")
	case !cfg.Integrations.GitHub && ghFound:
		line(bang, "github", "gh available but disabled — set integrations.github = true")
	default:
		line(dot, "github", "disabled")
	}

	linearKey := os.Getenv("LINEAR_API_KEY")
	if linearKey == "" {
		linearKey = os.Getenv("LINEAR_KEY")
	}
	if linearKey == "" {
		envPath := filepath.Join(home, ".config", "tnt", ".env")
		if data, err := os.ReadFile(envPath); err == nil {
			for _, l := range strings.Split(string(data), "\n") {
				if v, ok := strings.CutPrefix(l, "LINEAR_API_KEY="); ok {
					linearKey = v
				} else if v, ok := strings.CutPrefix(l, "LINEAR_KEY="); ok {
					linearKey = v
				}
			}
		}
	}
	hasKey := linearKey != ""
	switch {
	case cfg.Integrations.Linear && hasKey:
		line(tick, "linear", "enabled · API key set")
	case cfg.Integrations.Linear && !hasKey:
		line(cross, "linear", "enabled but no API key — set LINEAR_API_KEY or add to ~/.config/tnt/.env")
	case !cfg.Integrations.Linear && hasKey:
		line(bang, "linear", "API key set but disabled — set integrations.linear = true")
	default:
		line(dot, "linear", "disabled")
	}

	_, nvimFound := binary("nvim")
	switch {
	case cfg.Session.Neovim && nvimFound:
		line(tick, "neovim", "enabled · nvim found")
	case cfg.Session.Neovim && !nvimFound:
		line(cross, "neovim", "enabled in config but nvim not found in PATH")
	case !cfg.Session.Neovim && nvimFound:
		line(bang, "neovim", "nvim available but disabled — set session.neovim = true")
	default:
		line(dot, "neovim", "disabled")
	}

	if cfg.Session.SaveRestore {
		line(tick, "session", "save_restore enabled")
	} else {
		line(dot, "session", "save_restore disabled")
	}

	section("diff tools")

	for _, tool := range []string{"fzf", "delta", "gawk"} {
		if _, found := binary(tool); found {
			line(tick, tool, "found")
		} else {
			line(dot, tool, "not found — needed for tnt diff")
		}
	}

	fmt.Println()
}
