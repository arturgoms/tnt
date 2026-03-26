# tnt

Go TUI + CLI for tmux-native agent orchestration. Manages sessions, worktrees, AI agents, todos, plans, and services.

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
│   ├── close.go                    # worktree close logic
│   ├── kill.go                     # session kill (save + switch + kill)
│   ├── save.go                     # session save command
│   ├── status.go                   # tmux status bar segment
│   └── notify.go                   # TTL-based notification system
└── internal/                       # shared packages
    ├── agents/agents.go            # agent detection (scan tmux panes)
    ├── config/config.go            # TOML config loader
    ├── git/git.go                  # git helpers
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
- `tnt session`
  - `tnt session save` → `runSave()` (in save.go)
  - `tnt session kill` → `runSessionKill()`
  - `tnt session notify` → `runNotify()`
  - `tnt session status` → `runStatus()`

#### `picker.go` — Main TUI (1777 lines)

The central view. Manages multiple states:

```
stateBrowse      — repo list + dashboard (default)
stateDetail      — drill-in to windows/branches
stateRestore     — restore prompt for saved sessions
stateTodo        — embedded todo view
stateAgent       — embedded agent view
stateBranch      — embedded branch picker
stateLayout      — embedded layout picker
stateNewSession  — new session name input
stateNewSessionGit — git init prompt
```

**Key types:**
- `pickerModel` — main model holding all state
- `tmuxContext` — detected once at startup (session, worktree, workdir, branch)
- `repoItem` — list item wrapping `scanner.Repo`

**Embedded views:** Todo, agent, branch, and layout models are held as fields on
`pickerModel`. When entering a view (e.g. `enterTodoState()`), the list dims and the
right panel shows the embedded model's `View()`. `wantsBack` flag signals return to browse.

**Dashboard:** `renderTodoSection()` and `renderAgentSection()` render the right panel.
Both accept a `dimmed` param for unfocused state. Agents load async via `loadAgentsCmd`.

**Workspace cycling:** `w` key cycles through workspace names. `rebuildListItems()` filters
inactive repos by workspace. Active sessions always show regardless.

**Delegate management:** `makeDimDelegate()` and `makeActiveDelegate()` return styled
list delegates. Applied via `m.list.SetDelegate()` in state transition methods.

#### `todo.go` — Todo TUI Model (1002 lines)

Full bubbletea model with 7 states:

```
todoList         — main list with cursor
todoAddText      — text input for new todo
todoAddProject   — project input (after text)
todoEditPicker   — field selector for edit
todoEditValue    — value input for edit
todoConfirmDelete — y/n confirmation
todoRemind       — reminder time input
```

**Hierarchical grouping:** Todos grouped by repo → worktree using `todos.RepoGroup`.
Repo headers and worktree sub-headers are selectable rows.

**Embedded mode:** When `embedded=true`, `esc` sets `wantsBack=true` instead of quitting.
Non-key messages (textinput blink) forwarded in the `default` switch case.

#### `agent.go` — Agent Roster (309 lines)

Lists detected opencode agents with status icons. Supports embedded mode.
Agent detection runs async — `newAgentModelWithList()` takes pre-loaded agents
from the dashboard, `loadAgentRefreshCmd` refreshes in background.

#### `branch.go` — Branch Picker (450 lines)

Lists worktrees (jump/open) and branches (checkout/create). Entries load async via
`loadBranchEntriesCmd`. `handleBranchDeferred()` runs after TUI exits to execute
tmux operations (jump, create worktree, run layout).

#### `plan.go` — Plan System (408 lines)

**`runPlanUpdate()`**: Parses flags, marks plan steps complete in plan.md,
appends structured entries to comms.md, fires cross-agent alerts.

**`alertAgent()`**: Finds target repo's opencode pane via `agents.Detect()`,
sends message via tmux `send-keys`.

**`runPlanInbox()`**: Parses comms.md for `**Question for {repo}**:` and
`**Blocked on {repo}**:` entries targeting the current repo.

#### `status.go` — Tmux Status Bar (145 lines)

Called every few seconds by tmux. Detects agents, tracks state transitions
via timestamp files, fires notifications on running→waiting/done transitions,
outputs formatted segment string.

#### `session/session.go` — Session Persistence (473 lines)

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
- `PathsConfig` — plans, skills, state, layouts, projects, scripts
- `SearchConfig` — max_depth, default_workspace
- `WorkspaceConfig` — name + dirs[]
- `ThemeConfig` — ayu-dark hex colors

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

#### `session/` — Session Save/Restore

See `cmd/session.go` section above. This package handles the actual
pane capture, nvim session saving, opencode session detection, and
the restore replay logic.

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
`theme.Green`, etc. as `lipgloss.Color` values.
