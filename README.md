# tnt

Go TUI + CLI for tmux-native agent orchestration. Manages sessions, worktrees, AI agents, todos, plans, pull requests, and Linear issues.

Built with [bubbletea](https://github.com/charmbracelet/bubbletea), [lipgloss](https://github.com/charmbracelet/lipgloss), and [cobra](https://github.com/spf13/cobra).

## Build

```bash
make install    # builds and copies to ~/go/bin/tnt
```

## Code Structure

```
tnt/
├── main.go                         # entry point
├── cmd/                            # CLI commands + TUI views
│   ├── root.go                     # cobra command tree
│   ├── picker.go                   # main TUI (repo picker + dashboard)
│   ├── todo.go                     # todo TUI model (embedded in picker)
│   ├── todocli.go                  # todo CLI subcommands (add/toggle/delete/etc)
│   ├── agent.go                    # agent roster TUI model (embedded in picker)
│   ├── branch.go                   # branch/worktree picker TUI model
│   ├── layout.go                   # layout picker TUI model
│   ├── run.go                      # service manager + worktree picker
│   ├── plan.go                     # plan update + inbox + cross-agent alerts
│   ├── plans.go                    # plan/task progress TUI model (embedded in picker)
│   ├── prs.go                      # pull request TUI model (embedded in picker)
│   ├── close.go                    # worktree close logic
│   ├── kill.go                     # session kill (save + switch + kill)
│   ├── save.go                     # session save command
│   ├── status.go                   # tmux status bar segment
│   └── notify.go                   # TTL-based notification system
└── internal/                       # shared packages
    ├── agents/agents.go            # agent detection (scan tmux panes)
    ├── config/config.go            # TOML config loader
    ├── git/git.go                  # git helpers
    ├── linear/linear.go            # Linear API client (assigned issues + worktree matching)
    ├── plans/plans.go              # opencode task progress reader (T-*.json files)
    ├── prs/prs.go                  # GitHub PR + check-run loader (gh CLI)
    ├── recents/recents.go          # recently opened repos list
    ├── scanner/scanner.go          # repo scanner (workspaces, git info)
    ├── session/session.go          # session save/restore (nvim, opencode)
    ├── theme/theme.go              # ayu-dark color palette
    ├── tmux/tmux.go                # tmux command wrappers
    ├── todos/todos.go              # todo CRUD + hierarchical grouping
    ├── tui/app.go                  # App struct (Config + Theme)
    ├── tui/keys.go                 # shared key bindings
    └── worktree/worktree.go        # worktree management (create, jump, layout)
```

## Package Reference

### `cmd/` — Commands and TUI Views

All TUI models follow the bubbletea pattern: `Init()`, `Update(msg) (Model, Cmd)`, `View() string`.

#### `root.go` — Command Tree

Defines the cobra command hierarchy:

- `tnt` → `runPicker()` (default), `runTodo()` (with `--todo`)
- `tnt version` → prints build version and config path
- `tnt worktree` → `runBranchPicker()` (alias: `tnt wt`)
  - `tnt worktree close` → `runClose()`
  - `tnt worktree layout` → `runLayoutPicker()`
  - `tnt worktree run` → `runRunWindow()`
- `tnt agent` → `runAgentRoster()`
  - `tnt agent jump` → `runAgentJump()`
  - `tnt agent cycle` → `runAgentCycle()`
- `tnt todo` → `runTodo()`
  - `tnt todo add/toggle/delete/edit/list/get/cron`
- `tnt plan`
  - `tnt plan update` → `runPlanUpdate()`
  - `tnt plan inbox` → `runPlanInbox()`
  - `tnt plan open` → stub
  - `tnt plan dashboard` → stub
- `tnt session`
  - `tnt session save` → `runSave()` (in save.go)
  - `tnt session kill` → `runSessionKill()`
  - `tnt session notify` → `runNotify()`
  - `tnt session status` → `runStatus()`
- `tnt diff` → stub (actual diff browsing is handled by `diff-view.sh`)

#### `picker.go` — Main TUI

The central view. Manages multiple states:

```
stateBrowse        — repo list + dashboard (default)
stateDetail        — drill-in to windows/branches
stateRestore       — restore prompt for saved sessions
stateRepoContext   — per-repo panel: plan tasks + PRs for selected repo
stateOverview      — global panel: todos + agents + review PRs + Linear issues
stateBranch        — embedded branch picker
stateLayout        — embedded layout picker
stateNewSession    — new session name input
stateNewSessionGit — git init prompt
```

**Key types:**
- `pickerModel` — main model holding all state
- `tmuxContext` — detected once at startup (session, worktree, workdir, branch)
- `repoItem` — list item wrapping `scanner.Repo`

**Tab cycle:** `tab` cycles through three focus modes:
```
stateBrowse → stateRepoContext → stateOverview → stateBrowse
```

**`stateRepoContext`** — right panel shows plan task progress and PRs for the currently
selected repo. Both sections load async; focus cycles with `tab` between them.

**`stateOverview`** — right panel shows a global dashboard: todos, agent status, PRs
where your review is requested, and Linear issues assigned to you. All data loads async
with caching (`prCacheTTL = 60s`, `reviewPRCacheTTL = 60s`, `linearCacheTTL = 120s`).

**Embedded views:** Todo, agent, plan, and PR models are held as fields on `pickerModel`.
When entering a panel state, the repo list dims and the right panel shows the embedded
model's `View()`. The `wantsBack` flag signals return to browse.

**Dashboard sections (stateOverview):**
- **Todos** — grouped by repo/worktree
- **Agents** — running/waiting/idle status with colored icons
- **Review PRs** — PRs where `@me` review is requested (via `gh` CLI)
- **Linear issues** — your assigned in-progress/todo issues with worktree match indicators

**Dashboard sections (stateRepoContext):**
- **Plan tasks** — opencode task progress for the selected repo, grouped by branch
- **Pull requests** — your open PRs for the selected repo, expandable to show CI checks

**Workspace cycling:** `w` key cycles through workspace names. `rebuildListItems()` filters
inactive repos by workspace. Active sessions always show regardless.

**Delegate management:** `makeDimDelegate()` and `makeActiveDelegate()` return styled
list delegates. Applied via `m.list.SetDelegate()` in state transition methods.

**Column widths:**
- `width < 80`: single column
- `80 ≤ width ≤ 120`: 1/3 list, 2/3 right panel
- `width > 120`: 1/4 list, ~37% mid-panel, ~38% right panel

#### `todo.go` — Todo TUI Model

Full bubbletea model with 7 states:

```
todoList          — main list with cursor
todoAddText       — text input for new todo
todoAddProject    — project input (after text)
todoEditPicker    — field selector for edit
todoEditValue     — value input for edit
todoConfirmDelete — y/n confirmation
todoRemind        — reminder time input
```

**Hierarchical grouping:** Todos grouped by repo → worktree using `todos.RepoGroup`.
Repo headers and worktree sub-headers are selectable rows.

**Embedded mode:** When `embedded=true`, `esc` sets `wantsBack=true` instead of quitting.
Non-key messages (textinput blink) forwarded in the `default` switch case.

#### `agent.go` — Agent Roster

Lists detected opencode agents with status icons. Supports embedded mode.
Agent detection runs async — `newAgentModelWithList()` takes pre-loaded agents
from the dashboard, `loadAgentRefreshCmd` refreshes in background.

#### `branch.go` — Branch Picker

Lists worktrees (jump/open) and branches (checkout/create). Entries load async via
`loadBranchEntriesCmd`. `handleBranchDeferred()` runs after TUI exits to execute
tmux operations (jump, create worktree, run layout).

#### `plans.go` — Plan/Task Progress TUI Model

Embedded bubbletea model displaying opencode task progress per branch for the selected
repo. Data comes from `internal/plans` (reads `~/.config/opencode/tasks/{repo}/T-*.json`).

**States:** flat list of `BranchProgress` entries, expandable to show individual tasks.

**Keys:**

| Key | Action |
|-----|--------|
| `↑/↓` | Navigate branches |
| `→` | Expand branch to show task list |
| `←` | Collapse |
| `↵` | Expand (first press) / jump to branch (second press) |
| `r` | Refresh |
| `esc` | Back |

**Task status icons:**

| Icon | Color | Status |
|------|-------|--------|
| `✓` | Green | Completed |
| `◑` | Yellow | In progress |
| `○` | Gray | Pending |

Progress bar uses block characters: `███░░░░░░░ 3/10`.

#### `prs.go` — Pull Request TUI Model

Embedded bubbletea model displaying pull requests for the selected repo, loaded via
the `gh` CLI. Shows two sections: "my PRs" and "review requested". PRs are expandable
to show per-check-run CI status, loaded on demand.

**Keys:**

| Key | Action |
|-----|--------|
| `↑/↓` | Navigate |
| `→` | Expand PR → load and show check runs |
| `←` | Collapse |
| `↵` | Jump to PR (opens via `gh`) |
| `r` | Refresh |
| `esc` | Back |

**Check run icons:**

| Icon | Color | Meaning |
|------|-------|---------|
| `✓` | Green | All checks passed |
| `✗` | Red | One or more checks failed |
| `◑` | Yellow | In progress |

**Review decision icons:**

| Icon | Label | Color | Meaning |
|------|-------|-------|---------|
| `✓` | approved | Green | APPROVED |
| `●` | changes | Orange | CHANGES_REQUESTED |
| `⏳` | review | Gray | REVIEW_REQUIRED |
| `◌` | draft | Gray | Draft PR |

#### `plan.go` — Plan System

**`runPlanUpdate()`**: Parses flags, marks plan steps complete in plan.md,
appends structured entries to comms.md, fires cross-agent alerts.

**`alertAgent()`**: Finds target repo's opencode pane via `agents.Detect()`,
sends message via tmux `send-keys`.

**`runPlanInbox()`**: Parses comms.md for `**Question for {repo}**:` and
`**Blocked on {repo}**:` entries targeting the current repo.

#### `status.go` — Tmux Status Bar

Called every few seconds by tmux. Detects agents, tracks state transitions
via timestamp files, fires notifications on running→waiting/done transitions,
outputs formatted segment string.

#### `run.go` — Service Manager

Manages project services defined in `projects/{repo}/config.json`.

**`projectConfig` struct:**

```go
type projectConfig struct {
    DefaultLayout string    `json:"default_layout"`
    Env           string    `json:"env"`
    Hooks         hooks     `json:"hooks"`
    Services      []service `json:"services"`
}

type hooks struct {
    PostCreate []string `json:"post_create"`
    PreDelete  []string `json:"pre_delete"`
    PostDelete []string `json:"post_delete"`
}

type service struct {
    Name  string   `json:"name"`
    Run   string   `json:"run"`
    Cwd   string   `json:"cwd"`
    Setup []string `json:"setup"`
}
```

Hooks run as shell commands via `sh -c`. `setup` commands run before each service starts.

#### `session/session.go` — Session Persistence

Called by `runSessionKill()`, `runSave()`, and `runClose()`.

**Pane types:** `PaneNvim`, `PaneOpencode`, `PaneShell`, `PaneService`

**Process tree walking:** `detectNvimSocket()` and `detectOpencodeSession()` walk
pane PID → children → grandchildren to find socket paths and session IDs.
Falls back to querying opencode's SQLite DB by matching pane cwd to session directory.

**Save flow per nvim pane:**
1. Send `:Neotree close` (prevents restore issues)
2. Send `:mksession! {path}`
3. Record socket path

**Restore flow:**
- nvim: `nvim --listen {fresh_socket} -S {session.vim}`
- opencode: `NVIM_SOCKET_PATH={socket} opencode --port -s {session_id}`
- shell: `cd {cwd}`

Socket paths are always computed fresh on restore — never uses stale saved paths.

### `internal/` — Shared Packages

#### `agents/` — Agent Detection

`Detect(sessionFilter)` scans all tmux panes, finds opencode processes (direct command
or child process via `pgrep`), classifies status by reading pane content:
- **running**: contains braille spinners or "esc to interrupt"
- **waiting**: contains y/n prompts, approve/allow, numbered selections
- **idle**: everything else

#### `config/` — Configuration

Loads `config.toml` with path expansion (`~` → `$HOME`). Key structs:

```go
type Config struct {
    Paths      PathsConfig
    Search     SearchConfig
    Workspaces []WorkspaceConfig
    Theme      ThemeConfig
    Notify     NotifyConfig
    Layout     LayoutConfig
    Branch     BranchConfig
}
```

- `PathsConfig` — plans, skills, state, layouts, projects, scripts
- `SearchConfig` — max_depth, default_workspace
- `WorkspaceConfig` — name + dirs[]
- `ThemeConfig` — ayu-dark hex colors (bg, fg, gray, blue, cyan, green, orange, purple, red, yellow, dark, border)
- `NotifyConfig` — default_ttl, default_color, comms_color, comms_ttl
- `LayoutConfig` — default layout name
- `BranchConfig` — worktree_dir (default: `.worktrees`)

Default config path: `~/.config/tnt/config.toml`.

#### `linear/` — Linear API Client

Fetches your assigned Linear issues and cross-references them with local git worktrees.

**API key resolution** (in order):
1. `~/.config/tnt/.env` — line matching `LINEAR_KEY=<value>` (with optional `export` prefix and quotes)
2. `LINEAR_API_KEY` environment variable

**`LoadMyIssues(apiKey)`**: Queries the Linear GraphQL API for issues assigned to the
viewer with state type `started` or `unstarted`. Returns up to 30 issues sorted by:
state name (`In Progress → In Review → Todo`), then Linear priority (1=urgent first),
then identifier.

**`MatchWorktrees(issues, repos)`**: For each issue, checks all repo worktrees for a
branch name containing the issue identifier (e.g., `COU-112`). Attaches task progress
from `~/.config/opencode/tasks/{repo}/T-*.json` when a match is found.

**`Issue` fields:** Identifier, Title, StateType, StateName, Priority, URL, TeamKey

**`IssueWithWorktrees` fields:** embeds Issue + `[]TicketWorktree` (Repo, Branch, Done, Total, HasTasks)

#### `plans/` — Opencode Task Progress Reader

Reads task progress files written by opencode agents. **This is distinct from the
markdown-based `plan.md`/`comms.md` system used by `tnt plan update`.**

**Storage:** `~/.config/opencode/tasks/{repo}/T-*.json`

Each file is a JSON `Task`:
```json
{
  "id": "T-abc123",
  "subject": "Implement login flow",
  "status": "completed",
  "metadata": { "branch": "feat/auth" }
}
```

Status values: `completed`, `in_progress`, `pending`, `deleted` (deleted tasks are excluded from counts).

**`LoadForRepo(tasksDir, repo)`**: Reads all `T-*.json` files for a repo, groups by
branch (from `metadata.branch`), computes done/total counts per branch.

**`BranchProgress`**: `{ Branch, Tasks []Task, Done, Total }`

#### `prs/` — GitHub PR + Check-Run Loader

Wraps the `gh` CLI to load pull request data. Requires `gh` to be authenticated.

- **`LoadForRepo(repoPath)`** — lists your own open PRs (`--author @me`)
- **`LoadReviewRequested(repoPath)`** — lists PRs where your review is requested
- **`LoadChecksForPR(repoPath, number)`** — loads check run details for a specific PR
- **`LoadNotifications()`** — fetches GitHub notifications (currently unused in TUI)
- **`ChecksSummary(pr)`** → `(passed, failed, pending int)`
- **`ChecksIcon(passed, failed, pending)`** → `(icon, color string)`
- **`ReviewIcon(decision, isDraft)`** → `(icon, label, color string)`

#### `scanner/` — Repo Scanner

`Scan()` walks workspace dirs, finds git repos, detects active tmux sessions,
collects git info (branch, worktree count, last activity from `.git/index` mtime).
Orphan sessions (active tmux sessions not in any workspace) are detected and
resolved to their workspace by matching session path.

**No subprocess overhead:** Branch detection reads `.git/HEAD`, activity uses
`os.Stat` on `.git/index`. Zero git commands during scan.

#### `todos/` — Todo CRUD

JSON-based storage at `state/todos.json`.

**Hierarchical grouping:** `GroupByRepo()` returns `[]RepoGroup`, each containing
`[]WorktreeGroup` with active and done todos. `SplitProject()` splits
`"counterpart/int-112"` into repo + worktree.

**Atomic writes:** `Save()` writes to temp file then renames.

#### `worktree/` — Worktree Management

`RepoContext` holds git roots, session name, and config paths.
`ListEntries()` returns sorted entries: jump (active), open (on disk),
main, checkout (remote), local.

**Scaffolding on create:** `CreateWorktree()` also creates plan directory,
comms.md, and project config.json.

`OpenWorktreeWindow()` runs layout scripts. `JumpToWorktree()` does
`select-window` + `switch-client` to actually move the user.

#### `recents/` — Recently Opened

Simple JSON list at `state/recents.json`. `Add()` moves the name to front,
caps at 20 entries. Used by picker for sorting active sessions and by
kill/close for finding the next session to switch to.

#### `tmux/` — Tmux Wrappers

Thin wrappers around `exec.Command("tmux", ...)`:
- `Run()`, `ListWindows()`, `ListPanes()`
- `NewWindow()`, `SetWindowOption()`
- `SessionName()`, `HasSession()`

#### `theme/` — Color Palette

Ayu-dark colors loaded from config. All TUI views reference `theme.Blue`,
`theme.Green`, etc. as `lipgloss.Color` values. Full palette: `BG`, `FG`,
`Gray`, `Blue`, `Cyan`, `Green`, `Orange`, `Purple`, `Red`, `Yellow`, `Dark`, `Border`.

## Configuration

Full `config.toml` structure:

```toml
[paths]
plans    = "~/.config/tnt/plans"       # plan.md / comms.md files
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
dirs = ["~/projects/personal/", "~/.config/"]

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
default_ttl   = 30        # seconds TTL for general notifications
default_color = "#E6B450"
comms_color   = "#73D0FF" # color for cross-agent comms notifications
comms_ttl     = 120

[layout]
default = "dev"           # default layout name for new worktree windows

[branch]
worktree_dir = ".worktrees" # subdirectory inside repo for git worktrees
```

### Linear API Key

To enable the Linear integration, create `~/.config/tnt/.env`:

```bash
LINEAR_KEY=lin_api_xxxxxxxxxxxxxxxx
```

Or set the `LINEAR_API_KEY` environment variable. If the key is absent or empty,
Linear features silently disable — no error is shown.