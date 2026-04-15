# tnt

[![Build](https://github.com/arturgoms/tnt/actions/workflows/build.yml/badge.svg)](https://github.com/arturgoms/tnt/actions/workflows/build.yml)

`tnt` is a wrapper around tmux for managing multiple repositories as sessions, multiple worktrees as windows, and reusable layouts as pane arrangements.

The core product is the TUI and CLI for session, worktree, and layout management. Integrations like opencode, Linear, GitHub CLI, and Neovim are optional enhancements.

Built with [bubbletea](https://github.com/charmbracelet/bubbletea), [lipgloss](https://github.com/charmbracelet/lipgloss), and [cobra](https://github.com/spf13/cobra).

## Quick Start

```bash
# build and install the binary
make install

# set up ~/.config/tnt with config, layouts, and scripts
tnt install

# verify dependencies and configuration
tnt doctor

# open the main TUI
tnt

# open the worktree picker directly
tnt worktree

# create a window from a layout
tnt worktree layout
```

## Core Concepts

### Sessions

Each repository is managed as a tmux session.

### Worktrees

Each git worktree is managed as a tmux window inside the repository session.

### Layouts

Layouts are shell scripts that create a pane arrangement for a worktree window.

### Panes

Panes are just tmux panes. `tnt` can open shells, editors, service panes, or other tools inside them, but those tools are not required for the main TUI to work.

## Core Workflow

### Main TUI

Run `tnt` to open the main picker.

Core behavior:
- browse repositories
- switch to an existing tmux session
- restore a saved session
- open the worktree picker for the selected repo
- open the layout picker for the selected repo

### Worktree Management

`tnt worktree` opens the branch/worktree picker.

Supported flows:
- jump to an existing worktree window
- open an existing worktree on disk
- create a new worktree from a branch
- create a new branch and worktree
- close worktree windows with `tnt worktree close`

### Layouts

`tnt worktree layout` picks a layout and creates a new window for the current worktree.

Layouts are bash scripts in `layouts/` and receive:

```bash
layout.sh <workdir> <session> <branch> [after_wid] [color]
```

### Services

`tnt worktree run` manages project services for the current worktree.

Actions:
- `start`
- `stop`
- `restart`
- `switch`
- `pick`

### Project Config

Each repository can have a `config.json` at `~/.config/tnt/projects/{repo}/config.json`.

```json
{
  "default_layout": "dev",
  "env": "source .venv/bin/activate",
  "hooks": {
    "post_create": ["cp .env.example .env", "pip install -r requirements.txt"],
    "pre_delete": [],
    "post_delete": []
  },
  "services": [
    {
      "name": "backend",
      "run": "make run",
      "cwd": ".",
      "setup": ["make migrate"]
    },
    {
      "name": "frontend",
      "run": "npm run dev",
      "cwd": "frontend",
      "setup": ["npm install"]
    }
  ]
}
```

| Field | Description |
|---|---|
| `default_layout` | Layout script to use when opening a new session window |
| `env` | Shell command run before each service starts (e.g. activate a venv) |
| `hooks.post_create` | Commands run after every new worktree is created |
| `hooks.pre_delete` | Commands run before a worktree is removed |
| `hooks.post_delete` | Commands run after a worktree is removed |
| `services[].name` | Display name shown in the run window |
| `services[].run` | Command that starts the service |
| `services[].cwd` | Working directory relative to the worktree root |
| `services[].setup` | Commands run before `run` on each start (migrations, installs) |

A reference config is installed at `~/.config/tnt/projects/example/config.json` by `tnt install`.

### Session Persistence

`tnt session save` saves the current tmux session state.

`tnt session kill` saves the session, switches away, and kills it.

`tnt session status` renders a status segment for tmux.

`tnt session notify` sends and manages tmux notifications.

## Core Commands

| Command | Description |
|---|---|
| `tnt` | Main repo/session picker |
| `tnt worktree` | Branch and worktree picker |
| `tnt worktree close [branch]` | Close worktree windows and cleanup |
| `tnt worktree layout` | Pick a layout and create a new window |
| `tnt worktree run [action]` | Manage services for a worktree |
| `tnt session save` | Save current session state |
| `tnt session kill [name]` | Save and kill a tmux session |
| `tnt session notify ...` | Send/read/clear notifications |
| `tnt session status` | Print tmux status segment |
| `tnt version` | Print version and config path |
| `tnt install` | Set up config directory, layouts, scripts, and example files |
| `tnt doctor` | Check system dependencies and configuration |

## Tmux Setup

### Required settings

These two lines are the only hard requirements. Without them tnt cannot reliably name or look up windows.

```bash
# tnt names windows explicitly — prevent tmux from overriding them
setw -g automatic-rename off
set  -g allow-rename off
```

Add them to your `~/.tmux.conf`.

### Minimum version

`tnt` uses `display-popup -E` for its TUI overlays. This requires **tmux 3.2 or later**.

Check your version:

```bash
tmux -V
```

### Recommended bindings

All bindings are optional and fully configurable. This is a working example:

```bash
# Main repo/session picker
bind-key j display-popup -E -w 80% -h 80% "tnt"
bind-key s display-popup -E -w 80% -h 80% "tnt"

# Branch/worktree picker
bind-key b display-popup -E -w 60% -h 60% "tnt worktree"

# Layout picker
bind-key c display-popup -E -w 50% -h 40% "tnt worktree layout"

# Service manager
bind-key R display-popup -E -w 60% -h 40% "tnt worktree run pick"

# Close current worktree
bind-key C-s run-shell "tnt worktree close"

# Save and kill session
bind-key q   run-shell "tnt session kill"
bind-key C-q run-shell "tnt session kill"

# Todo manager
bind-key t display-popup -E -w 60% -h 50% "tnt todo"
```

### Status bar integration

`tnt session status` outputs a status segment for tmux notifications and (optionally) agent state. Add it to your `status-right`:

```bash
set -g status-interval 2
set -g status-right "#(tnt session status) ... "
```

### Window status with worktree colors and agent state

`tnt` sets two custom window options that you can reference in your status bar:

- `@worktree_color` — a color string assigned per worktree branch
- `@agent_state` — current agent status (`running`, `waiting`, `idle`)

Example window status format using both:

```bash
setw -g window-status-format \
  "#[fg=#{?#{@worktree_color},#{@worktree_color},default}] #I #W \
#{?#{==:#{@agent_state},running}, #[fg=green]◑,\
#{?#{==:#{@agent_state},waiting}, #[fg=yellow]●,}} "
```

These are optional — the TUI works without them. They only affect what you see in the tmux status bar.

### Known conflicts

**tmux-resurrect / tmux-continuum**

If you use `tmux-resurrect` or `tmux-continuum`, leave tnt's session save/restore disabled (it is off by default). Only enable it if you are not using those plugins.

```toml
# config.toml — choose one approach

# Option A: use tnt session save/restore
[session]
save_restore = true

# Option B: use tmux-resurrect or tmux-continuum (default)
[session]
save_restore = false
```

## Configuration

Default config path:

```bash
~/.config/tnt/config.toml
```

Example structure:

```toml
[paths]
plans    = "~/.config/tnt/plans"
tasks    = "~/.config/opencode/tasks"
skills   = "~/.config/tnt/skills"
state    = "~/.config/tnt/state"
layouts  = "~/.config/tnt/layouts"
projects = "~/.config/tnt/projects"
scripts  = "~/.config/tnt/scripts"

[search]
max_depth         = 1
default_workspace = "work"

[[workspace]]
name = "work"
dirs = ["~/projects/work/"]

[[workspace]]
name = "personal"
dirs = ["~/projects/personal/"]

[theme]
bg     = "#0D1017"
fg     = "#BFBDB6"
gray   = "#555E73"
blue   = "#39BAE6"
cyan   = "#95E6CB"
green  = "#AAD94C"
orange = "#FF8F40"
purple = "#D2A6FF"
red    = "#D95757"
yellow = "#E6B450"
dark   = "#141821"
border = "#1B1F29"

[notify]
default_ttl   = 30
default_color = "#E6B450"
comms_color   = "#73D0FF"
comms_ttl     = 120

[layout]
default = "dev"

[branch]
worktree_dir = ".worktrees"

[session]
save_restore = false  # enable tnt's session save/restore (conflicts with tmux-resurrect)
neovim       = false  # save/restore neovim sessions (requires save_restore = true)
opencode     = false  # save/restore opencode sessions (requires save_restore = true)

[integrations]
github   = false  # load pull requests via gh CLI
linear   = false  # load Linear issues
opencode = false  # agent detection, plan alerts, task progress
```

## Optional Integrations

These are enhancements. They should not be required for the main TUI to work.

### opencode

Used for agent roster, task progress panels, and plan alerts.

Enable in config:

```toml
[integrations]
opencode = true
```

Related commands: `tnt agent`, `tnt agent jump`, `tnt agent cycle`, `tnt plan ...`

### GitHub CLI

Used for pull request and check-run views. Requires `gh` installed and authenticated.

Enable in config:

```toml
[integrations]
github = true
```

### Linear

Used for assigned-issue panels and worktree matching.

Enable in config:

```toml
[integrations]
linear = true
```

Set your API key:

```bash
LINEAR_KEY=lin_api_xxxxxxxxxxxxxxxx  # in ~/.config/tnt/.env
# or
export LINEAR_API_KEY=lin_api_xxxxxxxxxxxxxxxx
```

### Neovim

Used for richer session save/restore when panes are running `nvim`.

Enable in config:

```toml
[session]
save_restore = true
neovim       = true
```

### Diff helper tools

The diff viewer is the remaining shell-based piece that has not been ported to Go yet.

These tools are only needed for that legacy diff workflow:
- `fzf`
- `delta`
- `gawk`

## Code Structure

```text
tnt/
├── main.go
├── cmd/
│   ├── root.go
│   ├── picker.go
│   ├── branch.go
│   ├── layout.go
│   ├── run.go
│   ├── close.go
│   ├── kill.go
│   ├── save.go
│   ├── status.go
│   ├── notify.go
│   ├── todo.go
│   ├── todocli.go
│   ├── agent.go
│   ├── plan.go
│   ├── plans.go
│   └── prs.go
└── internal/
    ├── scanner/
    ├── worktree/
    ├── session/
    ├── tmux/
    ├── config/
    ├── git/
    ├── todos/
    ├── recents/
    ├── theme/
    ├── agents/
    ├── linear/
    ├── plans/
    └── prs/
```

### Core packages

- `internal/scanner` — find repos and active sessions
- `internal/worktree` — create, list, and jump between worktrees
- `internal/session` — save and restore tmux session state
- `internal/tmux` — tmux command wrappers
- `internal/config` — config loading and path expansion
- `internal/git` — git helpers

### Optional integration packages

- `internal/agents` — opencode agent detection
- `internal/linear` — Linear API client
- `internal/plans` — opencode task progress reader
- `internal/prs` — GitHub PR loader via `gh`

## Build

```bash
make install
```

Requires:
- Go 1.21+
- tmux
- git

Optional tools:
- nvim
- opencode
- sqlite3
- gh

Optional diff helper tools:
- fzf
- delta
- gawk
