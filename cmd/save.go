package cmd

import (
	"fmt"

	"github.com/arturgomes/tnt/internal/session"
	"github.com/arturgomes/tnt/internal/tmux"
	"github.com/spf13/cobra"
)

var saveCmd = &cobra.Command{
	Use:   "save",
	Short: "Save current session windows for later restore",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, err := tmux.SessionName()
		if err != nil {
			return fmt.Errorf("not in a tmux session")
		}

		if err := session.Save(app.Config, name); err != nil {
			return fmt.Errorf("save: %w", err)
		}

		fmt.Printf("Session %q saved\n", name)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(saveCmd)
}
