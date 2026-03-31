package cmd

import (
	"fmt"
	"strings"

	"github.com/arturgomes/tnt/internal/prs"
	"github.com/arturgomes/tnt/internal/theme"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type prModel struct {
	prs       []prs.PR
	reviewPRs []prs.PR
	allRows   []prRow
	cursor    int
	expanded  map[int]bool
	theme     *theme.Theme
	embedded  bool
	wantsBack bool
	jumpTo    string
	openPR    int
	width     int
	height    int
	repoPath  string
	quitting  bool
}

type prRow struct {
	pr       prs.PR
	isHeader bool
	header   string
	isReview bool
}

type prRefreshMsg struct {
	mine   []prs.PR
	review []prs.PR
}

type prChecksLoadedMsg struct {
	number int
	checks []prs.CheckRun
}

func newPRModelWithList(t *theme.Theme, mine, review []prs.PR, repoPath string) prModel {
	m := prModel{
		theme:     t,
		prs:       mine,
		reviewPRs: review,
		expanded:  map[int]bool{},
		repoPath:  repoPath,
	}
	m.rebuildRows()
	return m
}

func (m *prModel) rebuildRows() {
	m.allRows = nil
	if len(m.prs) > 0 {
		m.allRows = append(m.allRows, prRow{isHeader: true, header: "my PRs"})
		for _, p := range m.prs {
			m.allRows = append(m.allRows, prRow{pr: p})
		}
	}
	if len(m.reviewPRs) > 0 {
		m.allRows = append(m.allRows, prRow{isHeader: true, header: "review requested"})
		for _, p := range m.reviewPRs {
			m.allRows = append(m.allRows, prRow{pr: p, isReview: true})
		}
	}
}

func loadPRRefreshCmd(repoPath string) tea.Cmd {
	return func() tea.Msg {
		mine, _ := prs.LoadForRepo(repoPath)
		review, _ := prs.LoadReviewRequested(repoPath)
		return prRefreshMsg{mine: mine, review: review}
	}
}

func (m prModel) Init() tea.Cmd {
	if len(m.allRows) == 0 {
		return loadPRRefreshCmd(m.repoPath)
	}
	return nil
}

func (m prModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case prRefreshMsg:
		m.prs = msg.mine
		m.reviewPRs = msg.review
		m.rebuildRows()
		if m.cursor >= len(m.allRows) {
			m.cursor = len(m.allRows) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		return m, nil

	case prChecksLoadedMsg:
		for i := range m.prs {
			if m.prs[i].Number == msg.number {
				m.prs[i].Checks = msg.checks
				break
			}
		}
		m.rebuildRows()
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("up"))):
			if m.cursor > 0 {
				m.cursor--
				if m.cursor >= 0 && m.allRows[m.cursor].isHeader {
					if m.cursor > 0 {
						m.cursor--
					} else {
						m.cursor++
					}
				}
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("down"))):
			if m.cursor < len(m.allRows)-1 {
				m.cursor++
				if m.cursor < len(m.allRows) && m.allRows[m.cursor].isHeader {
					if m.cursor < len(m.allRows)-1 {
						m.cursor++
					} else {
						m.cursor--
					}
				}
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("right"))):
			if m.cursor < len(m.allRows) && !m.allRows[m.cursor].isHeader {
				m.expanded[m.cursor] = true
				row := m.allRows[m.cursor]
				if !row.isReview && len(row.pr.Checks) == 0 && m.repoPath != "" {
					num := row.pr.Number
					return m, func() tea.Msg {
						checks, _ := prs.LoadChecksForPR(m.repoPath, num)
						return prChecksLoadedMsg{number: num, checks: checks}
					}
				}
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("left"))):
			if m.cursor < len(m.allRows) && !m.allRows[m.cursor].isHeader {
				m.expanded[m.cursor] = false
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			if m.cursor >= len(m.allRows) || m.allRows[m.cursor].isHeader {
				return m, nil
			}
			row := m.allRows[m.cursor]
			num := row.pr.Number
			if m.repoPath != "" && num > 0 {
				m.openPR = num
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
			return m, loadPRRefreshCmd(m.repoPath)

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

func (m prModel) View() string {
	if m.quitting {
		return ""
	}

	total := len(m.prs) + len(m.reviewPRs)
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(m.theme.Blue)).
		Padding(0, 1).
		Render(fmt.Sprintf("pull requests (%d)", total))

	if len(m.allRows) == 0 {
		empty := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Gray)).Padding(1, 2).Render("No pull requests found.")
		help := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Gray)).Padding(0, 2).Render("r refresh  esc back")
		return title + "\n\n" + empty + "\n\n" + help
	}

	maxH := m.height - 6
	if maxH < 3 {
		maxH = 3
	}

	var lines []string
	used := 0
	for i, row := range m.allRows {
		if used >= maxH {
			break
		}

		if row.isHeader {
			headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.theme.Cyan))
			lines = append(lines, "  "+headerStyle.Render(row.header))
			used++
			continue
		}

		selected := i == m.cursor
		if row.isReview {
			line := "  " + m.reviewSummaryLine(row.pr, false)
			if selected {
				line = fmt.Sprintf("  %s%s",
					lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Blue)).Bold(true).Render("> "),
					m.reviewSummaryLine(row.pr, true),
				)
			}
			lines = append(lines, line)
		} else {
			line := "  " + m.prSummaryLine(row.pr, false)
			if selected {
				line = fmt.Sprintf("  %s%s",
					lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Blue)).Bold(true).Render("> "),
					m.prSummaryLine(row.pr, true),
				)
			}
			lines = append(lines, line)
		}
		used++

		if !m.expanded[i] {
			continue
		}

		for _, check := range row.pr.Checks {
			if used >= maxH {
				break
			}
			icon := "◑"
			iconColor := m.theme.Yellow
			suffix := "  (in progress)"
			if check.Status == "COMPLETED" {
				suffix = ""
				if check.Conclusion == "SUCCESS" {
					icon = "✓"
					iconColor = m.theme.Green
				} else {
					icon = "✗"
					iconColor = m.theme.Red
				}
			}

			name := check.Name
			maxText := m.width - 24
			if maxText < 20 {
				maxText = 20
			}
			if len(name) > maxText {
				name = name[:maxText-3] + "..."
			}

			lines = append(lines, fmt.Sprintf("    %s %s%s",
				lipgloss.NewStyle().Foreground(lipgloss.Color(iconColor)).Render(icon),
				lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.FG)).Render(name),
				lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Gray)).Render(suffix),
			))
			used++
		}
	}

	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Gray)).
		Padding(0, 2).
		Render("↑/↓ move  → expand  ← collapse  ↵ jump  r refresh  esc back")

	return title + "\n\n" + strings.Join(lines, "\n") + "\n\n" + help
}

func (m prModel) prSummaryLine(pr prs.PR, selected bool) string {
	numColor := m.theme.Purple
	branchColor := m.theme.Purple
	mutedColor := m.theme.Gray
	if selected {
		numColor = m.theme.Blue
		branchColor = m.theme.Blue
		mutedColor = m.theme.Blue
	}

	number := lipgloss.NewStyle().Foreground(lipgloss.Color(numColor)).Render(fmt.Sprintf("#%d", pr.Number))
	branch := lipgloss.NewStyle().Foreground(lipgloss.Color(branchColor)).Render(pr.Branch)

	if pr.State == "MERGED" {
		merged := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Purple)).Render("◆ merged")
		if selected {
			merged = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Blue)).Render("◆ merged")
		}
		return fmt.Sprintf("%s %-16s  %s", number, branch, merged)
	}

	if pr.State == "CLOSED" {
		closed := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Red)).Render("✗ closed")
		if selected {
			closed = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Blue)).Render("✗ closed")
		}
		return fmt.Sprintf("%s %-16s  %s", number, branch, closed)
	}

	passed, failed, pending := prs.ChecksSummary(pr)
	icon, checkColor := prs.ChecksIcon(passed, failed, pending)
	total := passed + failed + pending

	checks := ""
	if total > 0 && icon != "" {
		color := m.theme.Gray
		switch checkColor {
		case "green":
			color = m.theme.Green
		case "red":
			color = m.theme.Red
		case "yellow":
			color = m.theme.Yellow
		}
		if selected {
			color = m.theme.Blue
		}
		checks = fmt.Sprintf("%s %d/%d", lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(icon), passed, total)
	}

	reviewIcon, reviewLabel, reviewColor := prs.ReviewIcon(pr.ReviewDecision, pr.IsDraft)
	review := ""
	if reviewIcon != "" {
		color := m.theme.Gray
		switch reviewColor {
		case "green":
			color = m.theme.Green
		case "orange":
			color = m.theme.Orange
		case "gray":
			color = m.theme.Gray
		}
		if selected {
			color = m.theme.Blue
		}
		review = fmt.Sprintf("%s %s", lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(reviewIcon), lipgloss.NewStyle().Foreground(lipgloss.Color(mutedColor)).Render(reviewLabel))
	}

	parts := []string{fmt.Sprintf("%s %-16s", number, branch)}
	if checks != "" {
		parts = append(parts, checks)
	}
	if review != "" {
		parts = append(parts, review)
	}
	return strings.Join(parts, "  ")
}

func (m prModel) reviewSummaryLine(pr prs.PR, selected bool) string {
	numColor := m.theme.Purple
	branchColor := m.theme.Purple
	authorColor := m.theme.Gray
	if selected {
		numColor = m.theme.Blue
		branchColor = m.theme.Blue
		authorColor = m.theme.Blue
	}

	number := lipgloss.NewStyle().Foreground(lipgloss.Color(numColor)).Render(fmt.Sprintf("#%d", pr.Number))
	branch := lipgloss.NewStyle().Foreground(lipgloss.Color(branchColor)).Render(pr.Branch)
	author := pr.Author.Login
	if pr.Author.Name != "" {
		author = pr.Author.Name
	}
	authorStr := lipgloss.NewStyle().Foreground(lipgloss.Color(authorColor)).Render(author)

	return fmt.Sprintf("%s %-16s  %s", number, branch, authorStr)
}
