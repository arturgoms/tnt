package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Paths        PathsConfig        `toml:"paths"`
	Search       SearchConfig       `toml:"search"`
	Workspaces   []WorkspaceConfig  `toml:"workspace"`
	Theme        ThemeConfig        `toml:"theme"`
	Notify       NotifyConfig       `toml:"notify"`
	Layout       LayoutConfig       `toml:"layout"`
	Branch       BranchConfig       `toml:"branch"`
	Session      SessionConfig      `toml:"session"`
	Integrations IntegrationsConfig `toml:"integrations"`
}

type PathsConfig struct {
	Plans    string `toml:"plans"`
	Tasks    string `toml:"tasks"`
	Skills   string `toml:"skills"`
	State    string `toml:"state"`
	Layouts  string `toml:"layouts"`
	Projects string `toml:"projects"`
	Scripts  string `toml:"scripts"`
}

type SearchConfig struct {
	Dirs             []string `toml:"dirs"`
	MaxDepth         int      `toml:"max_depth"`
	DefaultWorkspace string   `toml:"default_workspace"`
}

type WorkspaceConfig struct {
	Name string   `toml:"name"`
	Dirs []string `toml:"dirs"`
}

type ThemeConfig struct {
	BG     string `toml:"bg"`
	FG     string `toml:"fg"`
	Gray   string `toml:"gray"`
	Blue   string `toml:"blue"`
	Cyan   string `toml:"cyan"`
	Green  string `toml:"green"`
	Orange string `toml:"orange"`
	Purple string `toml:"purple"`
	Red    string `toml:"red"`
	Yellow string `toml:"yellow"`
	Dark   string `toml:"dark"`
	Border string `toml:"border"`
}

type NotifyConfig struct {
	DefaultTTL   int    `toml:"default_ttl"`
	DefaultColor string `toml:"default_color"`
	CommsColor   string `toml:"comms_color"`
	CommsTTL     int    `toml:"comms_ttl"`
}

type LayoutConfig struct {
	Default string `toml:"default"`
}

type BranchConfig struct {
	WorktreeDir string `toml:"worktree_dir"`
}

type SessionConfig struct {
	SaveRestore bool `toml:"save_restore"`
	Neovim      bool `toml:"neovim"`
	Opencode    bool `toml:"opencode"`
}

type IntegrationsConfig struct {
	GitHub   bool `toml:"github"`
	Linear   bool `toml:"linear"`
	Opencode bool `toml:"opencode"`
}

var DefaultConfigPath = filepath.Join(os.Getenv("HOME"), ".config", "tnt", "config.toml")

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath
	}

	cfg := defaults()

	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &cfg); err != nil {
			return nil, err
		}
	}

	cfg.expandPaths()
	return &cfg, nil
}

func (c *Config) expandPaths() {
	home := os.Getenv("HOME")
	expand := func(p string) string {
		return strings.Replace(p, "~", home, 1)
	}

	c.Paths.Plans = expand(c.Paths.Plans)
	c.Paths.Tasks = expand(c.Paths.Tasks)
	c.Paths.State = expand(c.Paths.State)
	c.Paths.Layouts = expand(c.Paths.Layouts)
	c.Paths.Projects = expand(c.Paths.Projects)
	c.Paths.Scripts = expand(c.Paths.Scripts)

	for i, d := range c.Search.Dirs {
		c.Search.Dirs[i] = expand(d)
	}

	for i := range c.Workspaces {
		for j, d := range c.Workspaces[i].Dirs {
			c.Workspaces[i].Dirs[j] = expand(d)
		}
	}
}

func defaults() Config {
	return Config{
		Paths: PathsConfig{
			Plans:    "~/.config/tnt/plans",
			Tasks:    "~/.config/opencode/tasks",
			State:    "~/.config/tnt/state",
			Layouts:  "~/.config/tnt/layouts",
			Projects: "~/.config/tnt/projects",
			Scripts:  "~/.config/tnt/scripts",
		},
		Search: SearchConfig{MaxDepth: 1},
		Theme: ThemeConfig{
			BG: "#0D1017", FG: "#BFBDB6", Gray: "#555E73",
			Blue: "#39BAE6", Cyan: "#95E6CB", Green: "#AAD94C",
			Orange: "#FF8F40", Purple: "#D2A6FF", Red: "#D95757",
			Yellow: "#E6B450", Dark: "#141821", Border: "#1B1F29",
		},
		Notify: NotifyConfig{
			DefaultTTL: 30, DefaultColor: "#E6B450",
			CommsColor: "#73D0FF", CommsTTL: 120,
		},
		Layout: LayoutConfig{Default: "dev"},
		Branch: BranchConfig{WorktreeDir: ".worktrees"},
	}
}
