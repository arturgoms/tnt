package cmd

import (
	"fmt"
	"os"

	"github.com/arturgomes/tnt/internal/config"
	"github.com/arturgomes/tnt/internal/tui"
	"github.com/spf13/cobra"
)

var (
	Version  = "dev"
	cfgPath  string
	todoFlag bool
	app      *tui.App
)

var rootCmd = &cobra.Command{
	Use:   "tnt",
	Short: "tmux-native agent orchestration",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		app = tui.NewApp(cfg)
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		if todoFlag {
			runTodo()
			return
		}
		runPicker()
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "", "config file (default: ~/.config/dotflow/tnt/config.toml)")
	rootCmd.Flags().BoolVar(&todoFlag, "todo", false, "open todo manager")

	planCmd.AddCommand(planUpdateCmd)
	planCmd.AddCommand(planOpenCmd)
	planCmd.AddCommand(planDashboardCmd)

	agentCmd.AddCommand(agentJumpCmd)
	agentCmd.AddCommand(agentCycleCmd)

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(branchCmd)
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(closeCmd)
	rootCmd.AddCommand(layoutCmd)
	initTodoCLI()
	rootCmd.AddCommand(todoCmd)
	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(notifyCmd)
	rootCmd.AddCommand(statusCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func stub(name string) {
	fmt.Printf("tnt %s — not yet implemented\n", name)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version and config path",
	Run: func(cmd *cobra.Command, args []string) {
		path := cfgPath
		if path == "" {
			path = config.DefaultConfigPath
		}
		fmt.Printf("tnt %s\nconfig: %s\n", Version, path)
	},
}

var branchCmd = &cobra.Command{
	Use:   "branch",
	Short: "Branch/worktree picker",
	Run: func(cmd *cobra.Command, args []string) {
		runBranchPicker()
	},
}

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Plan dashboard and comms",
}

var planUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Post to comms.md",
	Run: func(cmd *cobra.Command, args []string) {
		stub("plan update")
	},
}

var planOpenCmd = &cobra.Command{
	Use:   "open",
	Short: "View current plan",
	Run: func(cmd *cobra.Command, args []string) {
		stub("plan open")
	},
}

var planDashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Plan progress + comms across repos",
	Run: func(cmd *cobra.Command, args []string) {
		stub("plan dashboard")
	},
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Agent roster and management",
	Run: func(cmd *cobra.Command, args []string) {
		runAgentRoster()
	},
}

var agentJumpCmd = &cobra.Command{
	Use:   "jump",
	Short: "Jump to first waiting agent",
	Run: func(cmd *cobra.Command, args []string) {
		runAgentJump()
	},
}

var agentCycleCmd = &cobra.Command{
	Use:   "cycle",
	Short: "Cycle through active agent sessions",
	Run: func(cmd *cobra.Command, args []string) {
		runAgentCycle()
	},
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Service manager (start/stop/restart/pick)",
	Run: func(cmd *cobra.Command, args []string) {
		stub("run")
	},
}

var closeCmd = &cobra.Command{
	Use:   "close [branch]",
	Short: "Close worktree windows and cleanup",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runClose(args)
	},
}

var layoutCmd = &cobra.Command{
	Use:   "layout",
	Short: "Layout picker",
	Run: func(cmd *cobra.Command, args []string) {
		runLayoutPicker()
	},
}

var todoCmd = &cobra.Command{
	Use:   "todo",
	Short: "Todo manager",
	Run: func(cmd *cobra.Command, args []string) {
		runTodo()
	},
}

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Commit/file/hunk diff browser",
	Run: func(cmd *cobra.Command, args []string) {
		stub("diff")
	},
}

var notifyCmd = &cobra.Command{
	Use:                "notify [message] [--color COLOR] [--ttl SECONDS] [--read] [--clear]",
	Short:              "Send/read/clear tmux status bar notifications",
	Args:               cobra.ArbitraryArgs,
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		runNotify(args)
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Status bar segment for tmux",
	Run: func(cmd *cobra.Command, args []string) {
		stub("status")
	},
}
