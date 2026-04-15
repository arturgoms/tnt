package cmd

import (
	"fmt"
	"strings"

	"github.com/arturgoms/tnt/internal/plans"
	"github.com/arturgoms/tnt/internal/theme"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type planModel struct {
	plans     []plans.BranchProgress
	cursor    int
	expanded  map[int]bool
	theme     *theme.Theme
	embedded  bool
	wantsBack bool
	jumpTo    string
	width     int
	height    int
	tasksDir  string
	repo      string
	quitting  bool
}

type planRefreshMsg struct {
	list []plans.BranchProgress
}

func newPlanModelWithList(t *theme.Theme, list []plans.BranchProgress, tasksDir, repo string) planModel {
	return planModel{
		theme:    t,
		plans:    list,
		expanded: map[int]bool{},
		tasksDir: tasksDir,
		repo:     repo,
	}
}

func loadPlanRefreshCmd(tasksDir, repo string) tea.Cmd {
	return func() tea.Msg {
		return planRefreshMsg{list: plans.LoadForRepo(tasksDir, repo)}
	}
}

func (m planModel) Init() tea.Cmd {
	if len(m.plans) == 0 {
		return loadPlanRefreshCmd(m.tasksDir, m.repo)
	}
	return nil
}

func (m planModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case planRefreshMsg:
		m.plans = msg.list
		if m.cursor >= len(m.plans) {
			m.cursor = len(m.plans) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("up"))):
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("down"))):
			if m.cursor < len(m.plans)-1 {
				m.cursor++
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("right"))):
			if len(m.plans) > 0 {
				m.expanded[m.cursor] = true
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("left"))):
			if len(m.plans) > 0 {
				m.expanded[m.cursor] = false
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			if len(m.plans) == 0 {
				return m, nil
			}
			if !m.expanded[m.cursor] {
				m.expanded[m.cursor] = true
				return m, nil
			}
			m.jumpTo = m.plans[m.cursor].Branch
			m.quitting = true
			return m, tea.Quit

		case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
			return m, loadPlanRefreshCmd(m.tasksDir, m.repo)

		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
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
	}

	return m, nil
}

func (m planModel) View() string {
	if m.quitting {
		return ""
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(m.theme.Blue)).
		Padding(0, 1).
		Render(fmt.Sprintf("plans (%d branches)", len(m.plans)))

	if len(m.plans) == 0 {
		empty := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Gray)).Padding(1, 2).Render("No plan tasks found.")
		help := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Gray)).Padding(0, 2).Render("r refresh  esc back")
		return title + "\n\n" + empty + "\n\n" + help
	}

	maxH := m.height - 6
	if maxH < 3 {
		maxH = 3
	}

	visibleTop := 0
	for i := 0; i < m.cursor; i++ {
		rowH := 1
		if m.expanded[i] {
			for _, task := range m.plans[i].Tasks {
				if task.Status == "deleted" {
					continue
				}
				rowH++
			}
			if m.plans[i].Total == 0 {
				rowH++
			}
		}
		if rowHeightRange(m.plans, m.expanded, visibleTop, i) > maxH {
			visibleTop++
		}
		_ = rowH
	}

	var lines []string
	used := 0
	for i := visibleTop; i < len(m.plans); i++ {
		if used >= maxH {
			break
		}

		bp := m.plans[i]
		selected := i == m.cursor
		bar := progressBar(bp.Done, bp.Total, 10)
		summary := fmt.Sprintf("%s %d/%d", bar, bp.Done, bp.Total)
		line := fmt.Sprintf("  %s  %s", lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Purple)).Render(bp.Branch), lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Gray)).Render(summary))
		if selected {
			line = fmt.Sprintf("  %s%s  %s",
				lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Blue)).Bold(true).Render("> "),
				lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Blue)).Bold(true).Render(bp.Branch),
				lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Blue)).Bold(true).Render(summary),
			)
		}
		lines = append(lines, line)
		used++

		if !m.expanded[i] {
			continue
		}

		if bp.Total == 0 {
			if used >= maxH {
				break
			}
			lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Gray)).Render("      no tasks"))
			used++
			continue
		}

		for _, task := range bp.Tasks {
			if task.Status == "deleted" {
				continue
			}
			if used >= maxH {
				break
			}

			icon := "○"
			iconColor := m.theme.Gray
			suffix := ""
			switch task.Status {
			case "completed":
				icon = "✓"
				iconColor = m.theme.Green
			case "in_progress":
				icon = "◑"
				iconColor = m.theme.Yellow
				suffix = "  (in_progress)"
			case "pending":
				suffix = "  (pending)"
			}

			text := task.Subject
			maxText := m.width - 18
			if maxText < 20 {
				maxText = 20
			}
			if len(text) > maxText {
				text = text[:maxText-3] + "..."
			}

			lines = append(lines, fmt.Sprintf("    %s %s%s",
				lipgloss.NewStyle().Foreground(lipgloss.Color(iconColor)).Render(icon),
				lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.FG)).Render(text),
				lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Gray)).Render(suffix),
			))
			used++
		}
		if used >= maxH {
			break
		}
	}

	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Gray)).
		Padding(0, 2).
		Render("↑/↓ move  → expand  ← collapse  ↵ jump  r refresh  esc back")

	return title + "\n\n" + strings.Join(lines, "\n") + "\n\n" + help
}

func rowHeightRange(list []plans.BranchProgress, expanded map[int]bool, start, end int) int {
	total := 0
	for i := start; i <= end && i < len(list); i++ {
		total++
		if !expanded[i] {
			continue
		}
		added := 0
		for _, task := range list[i].Tasks {
			if task.Status == "deleted" {
				continue
			}
			added++
		}
		if added == 0 {
			added = 1
		}
		total += added
	}
	return total
}

func progressBar(done, total, width int) string {
	if width < 1 {
		width = 1
	}
	if total <= 0 {
		return strings.Repeat("░", width)
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	filled := (done * width) / total
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
