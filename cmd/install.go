package cmd

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var ExampleConfig []byte
var LayoutsFS embed.FS
var ProjectConfigExample []byte

var installCmd = &cobra.Command{
	Use:               "install",
	Short:             "Set up tnt config directory and required folders",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInstall()
	},
}

func runInstall() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home directory: %w", err)
	}

	base := filepath.Join(home, ".config", "tnt")

	tick := lipgloss.NewStyle().Foreground(lipgloss.Color("#AAD94C")).Render("✓")
	dot := lipgloss.NewStyle().Foreground(lipgloss.Color("#555E73")).Render("·")

	fmt.Println()

	entries := []struct {
		label string
		path  string
	}{
		{"config dir", base},
		{"state", filepath.Join(base, "state")},
		{"layouts", filepath.Join(base, "layouts")},
		{"projects", filepath.Join(base, "projects")},
		{"scripts", filepath.Join(base, "scripts")},
	}

	for _, e := range entries {
		if _, err := os.Stat(e.path); os.IsNotExist(err) {
			if err := os.MkdirAll(e.path, 0755); err != nil {
				return fmt.Errorf("create %s: %w", e.path, err)
			}
			fmt.Printf("  %s  created  %s\n", tick, e.path)
		} else {
			fmt.Printf("  %s  exists   %s\n", dot, e.path)
		}
	}

	cfgPath := filepath.Join(base, "config.toml")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := os.WriteFile(cfgPath, ExampleConfig, 0644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		fmt.Printf("  %s  created  %s\n", tick, cfgPath)
	} else {
		fmt.Printf("  %s  exists   %s\n", dot, cfgPath)
	}

	fmt.Println()

	layoutsDir := filepath.Join(base, "layouts")
	layoutFiles, lerr := fs.ReadDir(LayoutsFS, "layouts")
	if lerr != nil {
		return fmt.Errorf("read embedded layouts: %w", lerr)
	}
	for _, lf := range layoutFiles {
		if lf.IsDir() {
			continue
		}
		dest := filepath.Join(layoutsDir, lf.Name())
		if _, err := os.Stat(dest); err == nil {
			fmt.Printf("  %s  exists   %s\n", dot, dest)
			continue
		}
		data, err := fs.ReadFile(LayoutsFS, filepath.Join("layouts", lf.Name()))
		if err != nil {
			return fmt.Errorf("read layout %s: %w", lf.Name(), err)
		}
		if err := os.WriteFile(dest, data, 0755); err != nil {
			return fmt.Errorf("write layout %s: %w", lf.Name(), err)
		}
		fmt.Printf("  %s  created  %s\n", tick, dest)
	}

	exampleProject := filepath.Join(base, "project.config.example.json")
	if _, err := os.Stat(exampleProject); os.IsNotExist(err) {
		if err := os.WriteFile(exampleProject, ProjectConfigExample, 0644); err != nil {
			return fmt.Errorf("write project config example: %w", err)
		}
		fmt.Printf("  %s  created  %s\n", tick, exampleProject)
	} else {
		fmt.Printf("  %s  exists   %s\n", dot, exampleProject)
	}

	fmt.Println()
	fmt.Printf("  Edit %s to configure your workspaces.\n", cfgPath)
	fmt.Printf("  Copy %s to projects/{repo}/config.json for each repo.\n", exampleProject)
	fmt.Println()
	return nil
}
