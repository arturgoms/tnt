package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/arturgoms/tnt/internal/theme"
	"github.com/arturgoms/tnt/internal/todos"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- types ---

type todoState int

const (
	todoList todoState = iota
	todoAddText
	todoAddProject
	todoEditPicker
	todoEditValue
	todoConfirmDelete
	todoRemind
)

type listRow struct {
	isRepoHeader bool
	isWorktree   bool
	repo         string
	worktree     string
	project      string
	todo         *todos.Todo
	isDone       bool
}

type todoModel struct {
	file           *todos.TodoFile
	repoGroups     []todos.RepoGroup
	rows           []listRow
	cursor         int
	state          todoState
	theme          *theme.Theme
	stateDir       string
	input          textinput.Model
	editField      string
	editTodoID     string
	showDone       map[string]bool
	quitting       bool
	embedded       bool
	wantsBack      bool
	width          int
	height         int
	filterText     string
	filtering      bool
	deferredAction string
	deferredArg    string
}

func newTodoModel(t *theme.Theme, stateDir string) todoModel {
	ti := textinput.New()
	ti.CharLimit = 200
	ti.Width = 60

	m := todoModel{
		theme:    t,
		stateDir: stateDir,
		input:    ti,
		showDone: map[string]bool{},
	}
	m.reload()
	return m
}

func (m *todoModel) reload() {
	file, err := todos.LoadRaw(m.stateDir)
	if err != nil {
		file = &todos.TodoFile{}
	}
	m.file = file
	m.repoGroups = todos.GroupByRepo(file)
	m.buildRows()
}

func (m *todoModel) matchesFilter(text string) bool {
	if !m.filtering || m.filterText == "" {
		return true
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(m.filterText))
}

func (m *todoModel) buildRows() {
	var rows []listRow
	for _, rg := range m.repoGroups {
		hasAny := false
		for _, wt := range rg.Worktrees {
			for _, t := range wt.Active {
				if m.matchesFilter(t.Text) || m.matchesFilter(rg.Name) || m.matchesFilter(wt.Name) {
					hasAny = true
					break
				}
			}
			if !hasAny && m.showDone[rg.Name] {
				for _, t := range wt.Done {
					if m.matchesFilter(t.Text) || m.matchesFilter(rg.Name) || m.matchesFilter(wt.Name) {
						hasAny = true
						break
					}
				}
			}
			if hasAny {
				break
			}
		}
		if m.filtering && m.filterText != "" && !hasAny {
			continue
		}

		rows = append(rows, listRow{isRepoHeader: true, repo: rg.Name})

		multiWT := len(rg.Worktrees) > 1

		for _, wt := range rg.Worktrees {
			if multiWT || wt.Name != "" {
				rows = append(rows, listRow{isWorktree: true, repo: rg.Name, worktree: wt.Name, project: wt.Project})
			}

			for i := range wt.Active {
				t := wt.Active[i]
				if m.filtering && m.filterText != "" {
					if !m.matchesFilter(t.Text) && !m.matchesFilter(rg.Name) && !m.matchesFilter(wt.Name) {
						continue
					}
				}
				rows = append(rows, listRow{todo: &t, project: wt.Project, repo: rg.Name, worktree: wt.Name})
			}

			if m.showDone[rg.Name] {
				for i := range wt.Done {
					t := wt.Done[i]
					if m.filtering && m.filterText != "" {
						if !m.matchesFilter(t.Text) && !m.matchesFilter(rg.Name) && !m.matchesFilter(wt.Name) {
							continue
						}
					}
					rows = append(rows, listRow{todo: &t, project: wt.Project, repo: rg.Name, worktree: wt.Name, isDone: true})
				}
			}
		}
	}
	m.rows = rows

	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *todoModel) selectedTodo() *todos.Todo {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor].todo
}

func (m *todoModel) selectedProject() string {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	return m.rows[m.cursor].project
}

func (m *todoModel) selectedRepo() string {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	return m.rows[m.cursor].repo
}

func (m *todoModel) save() {
	todos.Save(m.stateDir, m.file)
	m.repoGroups = todos.GroupByRepo(m.file)
	m.buildRows()
}

func (m todoModel) Init() tea.Cmd {
	return nil
}

func (m todoModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = msg.Width - 20
		if m.input.Width < 30 {
			m.input.Width = 30
		}
		return m, nil

	case tea.KeyMsg:
		switch m.state {
		case todoList:
			return m.updateList(msg)
		case todoAddText:
			return m.updateAddText(msg)
		case todoAddProject:
			return m.updateAddProject(msg)
		case todoEditPicker:
			return m.updateEditPicker(msg)
		case todoEditValue:
			return m.updateEditValue(msg)
		case todoConfirmDelete:
			return m.updateConfirmDelete(msg)
		case todoRemind:
			return m.updateRemind(msg)
		}

	default:
		if m.state == todoAddText || m.state == todoAddProject || m.state == todoEditValue || m.state == todoRemind {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

// --- state handlers ---

func (m todoModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		return m.updateFilter(msg)
	}

	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("up"))):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("down"))):
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		t := m.selectedTodo()
		if t == nil {
			return m, nil
		}
		todos.Toggle(m.file, t.ID)
		m.save()
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("a"))):
		m.state = todoAddText
		m.input.SetValue("")
		m.input.Placeholder = "What needs to be done?"
		m.input.Focus()
		return m, textinput.Blink

	case key.Matches(msg, key.NewBinding(key.WithKeys("d"))):
		t := m.selectedTodo()
		if t == nil {
			return m, nil
		}
		m.state = todoConfirmDelete
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("e"))):
		t := m.selectedTodo()
		if t == nil {
			return m, nil
		}
		m.editTodoID = t.ID
		m.state = todoEditPicker
		m.cursor = 0 // reuse cursor for edit field picker
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("s"))):
		repo := m.selectedRepo()
		if repo == "" {
			return m, nil
		}
		m.showDone[repo] = !m.showDone[repo]
		m.buildRows()
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("n"))):
		t := m.selectedTodo()
		if t == nil {
			return m, nil
		}
		m.editTodoID = t.ID
		m.state = todoRemind
		m.input.SetValue("")
		m.input.Placeholder = "30m / 1h / 2h / tomorrow 9am"
		m.input.Focus()
		return m, textinput.Blink

	case key.Matches(msg, key.NewBinding(key.WithKeys("o"))):
		t := m.selectedTodo()
		if t == nil || t.Source == nil || t.Source.File == "" {
			return m, nil
		}
		m.deferredAction = "nvim"
		m.deferredArg = t.ID
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, key.NewBinding(key.WithKeys("i"))):
		t := m.selectedTodo()
		if t == nil {
			return m, nil
		}
		m.deferredAction = "opencode"
		m.deferredArg = t.ID
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
		m.filtering = true
		m.filterText = ""
		m.input.SetValue("")
		m.input.Placeholder = "filter..."
		m.input.Focus()
		return m, textinput.Blink

	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		if m.filterText != "" {
			m.filterText = ""
			m.buildRows()
			return m, nil
		}
		if m.embedded {
			m.wantsBack = true
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))):
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

func (m todoModel) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		m.filtering = false
		m.filterText = ""
		m.buildRows()
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		m.filtering = false
		// Keep filter applied, but stop input mode
		return m, nil

	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.filterText = m.input.Value()
		m.buildRows()
		return m, cmd
	}
}

func (m todoModel) updateAddText(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		m.state = todoList
		return m, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			m.state = todoList
			return m, nil
		}
		m.deferredArg = text
		m.state = todoAddProject
		defProj := todos.DefaultProject()
		m.input.SetValue("")
		m.input.Placeholder = fmt.Sprintf("project [%s]", defProj)
		m.input.Focus()
		return m, textinput.Blink
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

func (m todoModel) updateAddProject(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		m.state = todoList
		return m, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		project := strings.TrimSpace(m.input.Value())
		if project == "" {
			project = todos.DefaultProject()
		}
		// We need to retrieve the text from the previous step.
		// The text was in m.input before we switched to project.
		// We need to store it. Let me use editField as temp storage.
		// Actually let's fix this: store text in editField during addText→addProject transition.
		// For now, look back — we stored nothing. Let me use deferredArg.
		text := m.deferredArg // set during addText→addProject
		todos.Add(m.file, text, project, "")
		m.save()
		m.state = todoList
		m.deferredArg = ""
		return m, nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

var editFields = []struct {
	key   string
	label string
}{
	{"text", "Title"},
	{"description", "Description"},
	{"project", "Project"},
}

func (m todoModel) updateEditPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		m.state = todoList
		m.reload()
		return m, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("up"))):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("down"))):
		if m.cursor < len(editFields)-1 {
			m.cursor++
		}
		return m, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		t := todos.FindByID(m.file, m.editTodoID)
		if t == nil {
			m.state = todoList
			m.reload()
			return m, nil
		}
		field := editFields[m.cursor]
		m.editField = field.key
		m.state = todoEditValue
		switch field.key {
		case "text":
			m.input.SetValue(t.Text)
		case "description":
			m.input.SetValue(t.Description)
		case "project":
			m.input.SetValue(t.Project)
		}
		m.input.Placeholder = field.label
		m.input.Focus()
		m.input.CursorEnd()
		return m, textinput.Blink
	}
	return m, nil
}

func (m todoModel) updateEditValue(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		m.state = todoEditPicker
		return m, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		val := m.input.Value()
		t := todos.FindByID(m.file, m.editTodoID)
		if t != nil {
			switch m.editField {
			case "text":
				t.Text = val
			case "description":
				t.Description = val
			case "project":
				t.Project = val
			}
			m.save()
		}
		m.state = todoList
		m.reload()
		return m, nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

func (m todoModel) updateConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		t := m.selectedTodo()
		if t != nil {
			todos.Delete(m.file, t.ID)
			m.save()
		}
		m.state = todoList
		return m, nil
	case "n", "esc":
		m.state = todoList
		return m, nil
	}
	return m, nil
}

func (m todoModel) updateRemind(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		m.state = todoList
		return m, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		val := strings.TrimSpace(m.input.Value())
		if val == "" {
			m.state = todoList
			return m, nil
		}
		t := todos.FindByID(m.file, m.editTodoID)
		if t != nil {
			t.RemindAt = &val
			m.save()
		}
		m.state = todoList
		return m, nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

// --- view ---

func (m todoModel) View() string {
	if m.quitting {
		return ""
	}

	switch m.state {
	case todoAddText:
		return m.viewInput("Add todo", "enter confirm  esc cancel")
	case todoAddProject:
		return m.viewInput("Project", "enter confirm  esc cancel")
	case todoEditPicker:
		return m.viewEditPicker()
	case todoEditValue:
		return m.viewInput("Edit "+m.editField, "enter confirm  esc cancel")
	case todoConfirmDelete:
		return m.viewConfirmDelete()
	case todoRemind:
		return m.viewInput("Remind in", "enter confirm  esc cancel")
	}

	return m.viewList()
}

func (m todoModel) viewList() string {
	t := m.theme

	activeCount := todos.RepoActiveCount(m.repoGroups)

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Blue).
		Padding(0, 1).
		Render(fmt.Sprintf("todos (%d)", activeCount))

	maxH := m.height - 4
	if maxH < 1 {
		maxH = 1
	}

	// Compute visible window
	visStart := 0
	if m.cursor >= maxH {
		visStart = m.cursor - maxH + 1
	}

	var lines []string
	for i, row := range m.rows {
		if i < visStart {
			continue
		}
		if len(lines) >= maxH {
			break
		}

		isCursor := i == m.cursor

		if row.isRepoHeader {
			doneCount := 0
			for _, rg := range m.repoGroups {
				if rg.Name == row.repo {
					doneCount = rg.DoneTotal
					break
				}
			}
			headerStyle := lipgloss.NewStyle().Foreground(t.Purple).Bold(true)
			if isCursor {
				headerStyle = lipgloss.NewStyle().Foreground(t.Blue).Bold(true)
			}
			header := headerStyle.Render(row.repo)
			if doneCount > 0 {
				showLabel := "show"
				if m.showDone[row.repo] {
					showLabel = "hide"
				}
				header += lipgloss.NewStyle().
					Foreground(t.Gray).
					Render(fmt.Sprintf(" +%d done [s: %s]", doneCount, showLabel))
			}
			prefix := "  "
			if isCursor {
				prefix = lipgloss.NewStyle().Foreground(t.Blue).Bold(true).Render("> ")
			}
			lines = append(lines, prefix+header)
			continue
		}

		if row.isWorktree {
			wtStyle := lipgloss.NewStyle().Foreground(t.Cyan)
			if isCursor {
				wtStyle = lipgloss.NewStyle().Foreground(t.Blue).Bold(true)
			}
			label := row.worktree
			if label == "" {
				label = "(default)"
			}
			prefix := "    "
			if isCursor {
				prefix = "  " + lipgloss.NewStyle().Foreground(t.Blue).Bold(true).Render("> ")
			}
			lines = append(lines, prefix+wtStyle.Render(label))
			continue
		}

		bullet := lipgloss.NewStyle().Foreground(t.Yellow).Render("○")
		if row.isDone {
			bullet = lipgloss.NewStyle().Foreground(t.Green).Render("●")
		}

		text := row.todo.Text
		maxW := m.width - 14
		if maxW < 20 {
			maxW = 20
		}
		if len(text) > maxW {
			text = text[:maxW-3] + "..."
		}

		style := lipgloss.NewStyle().Foreground(t.FG)
		if row.isDone {
			style = lipgloss.NewStyle().Foreground(t.Gray).Strikethrough(true)
		}

		line := "      " + bullet + " " + style.Render(text)

		if isCursor {
			pointer := lipgloss.NewStyle().Foreground(t.Blue).Bold(true).Render("> ")
			line = "    " + pointer + bullet + " " +
				lipgloss.NewStyle().Foreground(t.Blue).Bold(true).Render(text)
		}

		lines = append(lines, line)
	}

	if len(m.rows) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(t.Gray).Padding(1, 2).Render("No todos yet. Press 'a' to add one."))
	}

	content := title + "\n\n" + strings.Join(lines, "\n")

	// Filter indicator
	if m.filtering {
		filterLine := lipgloss.NewStyle().Foreground(t.Yellow).Render("/ ") + m.input.View()
		content += "\n\n" + filterLine
	} else if m.filterText != "" {
		filterLine := lipgloss.NewStyle().Foreground(t.Yellow).
			Render(fmt.Sprintf("filter: %s  (/ to change, esc to clear)", m.filterText))
		content += "\n\n  " + filterLine
	}

	help := lipgloss.NewStyle().
		Foreground(t.Gray).
		Padding(0, 2).
		Render("a add  ↵ toggle  d delete  e edit  s show done  n remind  o nvim  i AI  / filter  esc quit")

	return content + "\n\n" + help
}

func (m todoModel) viewInput(title, helpText string) string {
	t := m.theme

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Blue).
		Padding(1, 2).
		Render(title)

	input := lipgloss.NewStyle().
		Padding(0, 2).
		Render(m.input.View())

	help := lipgloss.NewStyle().
		Foreground(t.Gray).
		Padding(1, 2).
		Render(helpText)

	return header + "\n" + input + "\n" + help
}

func (m todoModel) viewEditPicker() string {
	t := m.theme

	todo := todos.FindByID(m.file, m.editTodoID)
	if todo == nil {
		return "todo not found"
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Blue).
		Padding(1, 2).
		Render(fmt.Sprintf("Edit: %s", todo.Text))

	var lines []string
	for i, f := range editFields {
		var val string
		switch f.key {
		case "text":
			val = todo.Text
		case "description":
			val = todo.Description
			if val == "" {
				val = "(empty)"
			}
		case "project":
			val = todo.Project
		}

		label := fmt.Sprintf("  %s: %s", f.label, val)
		if i == m.cursor {
			label = lipgloss.NewStyle().Foreground(t.Blue).Bold(true).Render("> " + f.label + ": " + val)
		} else {
			label = lipgloss.NewStyle().Foreground(t.FG).Render("  " + f.label + ": " + val)
		}
		lines = append(lines, "  "+label)
	}

	help := lipgloss.NewStyle().
		Foreground(t.Gray).
		Padding(1, 2).
		Render("↵ select  esc back")

	return header + "\n" + strings.Join(lines, "\n") + "\n" + help
}

func (m todoModel) viewConfirmDelete() string {
	t := m.theme

	todo := m.selectedTodo()
	if todo == nil {
		return ""
	}

	prompt := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Yellow).
		Padding(1, 2).
		Render(fmt.Sprintf("Delete \"%s\"?", todo.Text))

	help := lipgloss.NewStyle().
		Foreground(t.Gray).
		Padding(0, 2).
		Render("[y]es  [n]o / esc")

	return prompt + "\n" + help
}

// --- deferred actions (after TUI exits) ---

func handleTodoDeferred(m todoModel) {
	if m.deferredAction == "" {
		return
	}

	t := todos.FindByID(m.file, m.deferredArg)
	if t == nil {
		return
	}

	switch m.deferredAction {
	case "nvim":
		openTodoInNvim(t)
	case "opencode":
		sendTodoToOpencode(t)
	}
}

func openTodoInNvim(t *todos.Todo) {
	if t.Source == nil || t.Source.File == "" {
		return
	}

	repo := t.Source.Repo
	wt := t.Source.Worktree
	if repo == "" {
		// Try to extract from project
		parts := strings.SplitN(t.Project, "/", 2)
		if len(parts) >= 1 {
			repo = parts[0]
		}
		if len(parts) >= 2 {
			wt = parts[1]
		}
	}

	winID := findWorktreeWindow(repo, wt)
	if winID == "" {
		return
	}

	nvimPane := findPaneByCmd(winID, "nvim")
	if nvimPane == "" {
		return
	}

	exec.Command("tmux", "send-keys", "-t", nvimPane, "Escape", "Escape").Run()
	exec.Command("tmux", "send-keys", "-t", nvimPane,
		fmt.Sprintf(":edit +%d %s", t.Source.Line, t.Source.File), "Enter").Run()
	exec.Command("tmux", "select-window", "-t", winID).Run()
	exec.Command("tmux", "select-pane", "-t", nvimPane).Run()
}

func sendTodoToOpencode(t *todos.Todo) {
	repo := ""
	wt := ""
	if t.Source != nil {
		repo = t.Source.Repo
		wt = t.Source.Worktree
	}
	if repo == "" {
		parts := strings.SplitN(t.Project, "/", 2)
		if len(parts) >= 1 && parts[0] != "inbox" {
			repo = parts[0]
		}
		if len(parts) >= 2 {
			wt = parts[1]
		}
	}

	var winID, ocPane string

	if wt != "" {
		winID = findWorktreeWindow(repo, wt)
		if winID != "" {
			ocPane = findOpencodePaneInWindow(winID)
		}
	}

	if ocPane == "" && repo != "" {
		windows, err := exec.Command("tmux", "list-windows", "-t", repo, "-F", "#{window_id}").Output()
		if err == nil {
			for _, wid := range strings.Split(strings.TrimSpace(string(windows)), "\n") {
				if wid == "" {
					continue
				}
				ocPane = findOpencodePaneInWindow(wid)
				if ocPane != "" {
					winID = wid
					break
				}
			}
		}
	}

	if ocPane == "" {
		return
	}

	msg := "TODO: " + t.Text
	if t.Description != "" {
		msg += " | Description: " + t.Description
	}
	if t.Source != nil && t.Source.File != "" {
		msg += fmt.Sprintf(" | File: %s:%d", t.Source.File, t.Source.Line)
	}

	exec.Command("tmux", "send-keys", "-t", ocPane, msg).Run()
	exec.Command("tmux", "select-window", "-t", winID).Run()
	exec.Command("tmux", "select-pane", "-t", ocPane).Run()
}

func findWorktreeWindow(repo, wt string) string {
	windows, err := exec.Command("tmux", "list-windows", "-t", repo,
		"-F", "#{window_id} #{@worktree}").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(windows)), "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		fullWt := parts[1]
		if fullWt == wt {
			return parts[0]
		}
		short := fullWt
		if idx := strings.LastIndex(fullWt, "/"); idx >= 0 {
			short = fullWt[idx+1:]
		}
		if short == wt {
			return parts[0]
		}
	}
	return ""
}

func findPaneByCmd(windowID, cmd string) string {
	panes, err := exec.Command("tmux", "list-panes", "-t", windowID,
		"-F", "#{pane_id} #{pane_current_command}").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(panes)), "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 && parts[1] == cmd {
			return parts[0]
		}
	}
	return ""
}

func findOpencodePaneInWindow(windowID string) string {
	pane := findPaneByCmd(windowID, "opencode")
	if pane != "" {
		return pane
	}
	// Fallback: check child processes
	panes, err := exec.Command("tmux", "list-panes", "-t", windowID,
		"-F", "#{pane_pid} #{pane_id}").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(panes)), "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		pid := parts[0]
		paneID := parts[1]
		out, err := exec.Command("pgrep", "-P", pid, "-f", "opencode").Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return paneID
		}
	}
	return ""
}

// --- entry point ---

func runTodo() {
	cfg := app.Config
	t := app.Theme

	m := newTodoModel(t, cfg.Paths.State)
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	final := result.(todoModel)
	handleTodoDeferred(final)
}
