package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/arturgomes/tnt/internal/todos"
	"github.com/spf13/cobra"
)

var (
	todoProject     string
	todoDescription string
	todoSource      string
	todoText        string
	todoRemindAt    string
	todoShowDone    bool
	todoJSON        bool
)

var todoAddCmd = &cobra.Command{
	Use:   "add [text]",
	Short: "Add a todo",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := app.Config
		file, err := todos.LoadRaw(cfg.Paths.State)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		project := todoProject
		if project == "" {
			project = todos.DefaultProject()
		}

		t := todos.Add(file, args[0], project, todoDescription)

		if todoSource != "" {
			src := parseSource(todoSource)
			t.Source = &src
		}

		if err := todos.Save(cfg.Paths.State, file); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("added: %s [%s]\n", t.Text, t.Project)
	},
}

var todoToggleCmd = &cobra.Command{
	Use:   "toggle [id]",
	Short: "Toggle a todo done/undone",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := app.Config
		file, err := todos.LoadRaw(cfg.Paths.State)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		t, ok := todos.Toggle(file, args[0])
		if !ok {
			fmt.Fprintf(os.Stderr, "todo not found: %s\n", args[0])
			os.Exit(1)
		}

		if err := todos.Save(cfg.Paths.State, file); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		state := "active"
		if t.Done {
			state = "done"
		}
		fmt.Printf("%s: %s [%s]\n", state, t.Text, t.ID)
	},
}

var todoDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a todo",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := app.Config
		file, err := todos.LoadRaw(cfg.Paths.State)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		if !todos.Delete(file, args[0]) {
			fmt.Fprintf(os.Stderr, "todo not found: %s\n", args[0])
			os.Exit(1)
		}

		if err := todos.Save(cfg.Paths.State, file); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("deleted: %s\n", args[0])
	},
}

var todoEditCmd = &cobra.Command{
	Use:   "edit [id]",
	Short: "Edit a todo",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := app.Config
		file, err := todos.LoadRaw(cfg.Paths.State)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		t := todos.FindByID(file, args[0])
		if t == nil {
			fmt.Fprintf(os.Stderr, "todo not found: %s\n", args[0])
			os.Exit(1)
		}

		if todoText != "" {
			t.Text = todoText
		}
		if todoDescription != "" {
			t.Description = todoDescription
		}
		if todoProject != "" {
			t.Project = todoProject
		}
		if todoRemindAt != "" {
			t.RemindAt = &todoRemindAt
		}

		if err := todos.Save(cfg.Paths.State, file); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("updated: %s\n", t.Text)
	},
}

var todoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List todos",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := app.Config
		groups := todos.Load(cfg.Paths.State)

		if todoJSON {
			file, _ := todos.LoadRaw(cfg.Paths.State)
			data, _ := json.MarshalIndent(file, "", "  ")
			fmt.Println(string(data))
			return
		}

		for _, rg := range groups {
			for _, wt := range rg.Worktrees {
				if todoProject != "" && wt.Project != todoProject {
					continue
				}
				if len(wt.Active) == 0 && (!todoShowDone || len(wt.Done) == 0) {
					continue
				}

				label := rg.Name
				if wt.Name != "" {
					label += "/" + wt.Name
				}
				fmt.Printf("\n%s\n", label)

				for _, t := range wt.Active {
					fmt.Printf("  ○ [%s] %s\n", t.ID, t.Text)
				}
				if todoShowDone {
					for _, t := range wt.Done {
						fmt.Printf("  ● [%s] %s\n", t.ID, t.Text)
					}
				}
			}
		}
	},
}

var todoGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get a single todo",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := app.Config
		file, err := todos.LoadRaw(cfg.Paths.State)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		t := todos.FindByID(file, args[0])
		if t == nil {
			fmt.Fprintf(os.Stderr, "todo not found: %s\n", args[0])
			os.Exit(1)
		}

		if todoJSON {
			data, _ := json.MarshalIndent(t, "", "  ")
			fmt.Println(string(data))
			return
		}
		fmt.Printf("[%s] %s\n  project: %s\n  done: %v\n", t.ID, t.Text, t.Project, t.Done)
		if t.Description != "" {
			fmt.Printf("  description: %s\n", t.Description)
		}
		if t.Source != nil && t.Source.File != "" {
			fmt.Printf("  source: %s:%d\n", t.Source.File, t.Source.Line)
		}
	},
}

var todoCronCmd = &cobra.Command{
	Use:   "cron",
	Short: "Check due reminders and fire notifications",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := app.Config
		file, err := todos.LoadRaw(cfg.Paths.State)
		if err != nil {
			return
		}

		now := time.Now()
		changed := false

		for i := range file.Todos {
			t := &file.Todos[i]
			if t.Done || t.RemindAt == nil || *t.RemindAt == "" {
				continue
			}

			remind, err := time.Parse(time.RFC3339, *t.RemindAt)
			if err != nil {
				continue
			}

			if remind.After(now) {
				continue
			}

			notifySend(cfg.Paths.State, fmt.Sprintf("todo: %s (%s)", t.Text, t.Project), "#E6B450", 120)
			t.RemindAt = nil
			changed = true
		}

		if changed {
			todos.Save(cfg.Paths.State, file)
		}
	},
}

func parseSource(s string) todos.Source {
	src := todos.Source{}
	parts := strings.SplitN(s, ":", 4)
	if len(parts) >= 1 {
		src.File = parts[0]
	}
	if len(parts) >= 2 {
		fmt.Sscanf(parts[1], "%d", &src.Line)
	}
	if len(parts) >= 3 {
		src.Repo = parts[2]
	}
	if len(parts) >= 4 {
		src.Worktree = parts[3]
	}
	return src
}

func initTodoCLI() {
	todoAddCmd.Flags().StringVar(&todoProject, "project", "", "project name")
	todoAddCmd.Flags().StringVar(&todoDescription, "description", "", "description")
	todoAddCmd.Flags().StringVar(&todoSource, "source", "", "source file:line[:repo:worktree]")

	todoEditCmd.Flags().StringVar(&todoText, "text", "", "new text")
	todoEditCmd.Flags().StringVar(&todoDescription, "description", "", "new description")
	todoEditCmd.Flags().StringVar(&todoProject, "project", "", "new project")
	todoEditCmd.Flags().StringVar(&todoRemindAt, "remind", "", "reminder time")

	todoListCmd.Flags().StringVar(&todoProject, "project", "", "filter by project")
	todoListCmd.Flags().BoolVar(&todoShowDone, "show-done", false, "include done todos")
	todoListCmd.Flags().BoolVar(&todoJSON, "json", false, "output as JSON")

	todoGetCmd.Flags().BoolVar(&todoJSON, "json", false, "output as JSON")

	todoCmd.AddCommand(todoAddCmd)
	todoCmd.AddCommand(todoToggleCmd)
	todoCmd.AddCommand(todoDeleteCmd)
	todoCmd.AddCommand(todoEditCmd)
	todoCmd.AddCommand(todoListCmd)
	todoCmd.AddCommand(todoGetCmd)
	todoCmd.AddCommand(todoCronCmd)
}
