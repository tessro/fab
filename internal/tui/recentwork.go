package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/tessro/fab/internal/daemon"
)

// RecentWork displays recently completed work for ambient awareness.
type RecentWork struct {
	width   int
	height  int
	commits []daemon.CommitInfo
}

// NewRecentWork creates a new recent work component.
func NewRecentWork() RecentWork {
	return RecentWork{}
}

// SetSize updates the component dimensions.
func (r *RecentWork) SetSize(width, height int) {
	r.width = width
	r.height = height
}

// SetCommits updates the list of recent commits.
func (r *RecentWork) SetCommits(commits []daemon.CommitInfo) {
	r.commits = commits
}

// View renders the recent work section.
func (r RecentWork) View() string {
	// Calculate inner dimensions (accounting for border)
	innerWidth := r.width - 2
	innerHeight := r.height - 2 - 1 // -2 for border, -1 for header
	if innerWidth < 1 {
		innerWidth = 1
	}
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Header
	header := paneTitleStyle.Width(innerWidth).Render("Recent")

	// Content
	var content string
	if len(r.commits) == 0 {
		content = recentWorkEmptyStyle.Width(innerWidth).Height(innerHeight).Render("No completed work yet")
	} else {
		var rows []string
		// Show as many commits as fit in the available height
		displayCount := innerHeight
		if displayCount > len(r.commits) {
			displayCount = len(r.commits)
		}
		for i := 0; i < displayCount; i++ {
			row := r.renderCommit(r.commits[i], innerWidth)
			rows = append(rows, row)
		}
		content = recentWorkContainerStyle.Width(innerWidth).Height(innerHeight).Render(strings.Join(rows, "\n"))
	}

	// Combine header and content
	inner := lipgloss.JoinVertical(lipgloss.Left, header, content)

	// Apply border (unfocused style - this section is non-interactive)
	return paneBorderStyle.Width(r.width - 2).Height(r.height - 2).Render(inner)
}

// renderCommit renders a single commit row.
func (r RecentWork) renderCommit(commit daemon.CommitInfo, width int) string {
	// Available content width is total width minus padding (1 on each side = 2)
	contentWidth := width - 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	// Build the display: checkmark + task/branch info + description
	// Format: "✓ #123 project description" or "✓ branch-name project description"
	checkmark := recentWorkCheckStyle.Render("✓")

	// Task ID or branch name
	var identifier string
	if commit.TaskID != "" {
		identifier = recentWorkTaskStyle.Render("#" + commit.TaskID)
	} else {
		// Use branch name, truncated
		branch := commit.Branch
		if len(branch) > 20 {
			branch = branch[:17] + "…"
		}
		identifier = recentWorkBranchStyle.Render(branch)
	}

	// Project name
	project := recentWorkProjectStyle.Render(commit.Project)

	// Compose left part (without description first)
	left := lipgloss.JoinHorizontal(lipgloss.Center,
		checkmark, " ",
		identifier, " ",
		project,
	)

	leftWidth := lipgloss.Width(left)

	// If too wide, truncate
	if leftWidth > contentWidth && contentWidth > 3 {
		// Just use the checkmark and truncated identifier
		maxIdentifierLen := contentWidth - 2 // account for checkmark and space
		if maxIdentifierLen > 1 {
			if commit.TaskID != "" {
				id := "#" + commit.TaskID
				if len(id) > maxIdentifierLen {
					id = id[:maxIdentifierLen-1] + "…"
				}
				identifier = recentWorkTaskStyle.Render(id)
			} else {
				branch := commit.Branch
				if len(branch) > maxIdentifierLen {
					branch = branch[:maxIdentifierLen-1] + "…"
				}
				identifier = recentWorkBranchStyle.Render(branch)
			}
			left = lipgloss.JoinHorizontal(lipgloss.Center, checkmark, " ", identifier)
			leftWidth = lipgloss.Width(left)
		}
	}

	// Add description if present and space available
	var description string
	if commit.Description != "" {
		remaining := contentWidth - leftWidth - 1 // -1 for space before description
		if remaining > 5 {                        // Only show if meaningful space available
			desc := commit.Description
			if len(desc) > remaining {
				desc = desc[:remaining-1] + "…"
			}
			description = " " + recentWorkDescriptionStyle.Render(desc)
		}
	}

	// Pad to full width
	leftWidth = lipgloss.Width(left + description)
	spacerWidth := contentWidth - leftWidth
	if spacerWidth < 0 {
		spacerWidth = 0
	}
	spacer := strings.Repeat(" ", spacerWidth)

	row := left + description + spacer

	// Apply row styling with full width
	return recentWorkRowStyle.Width(width).Render(row)
}
