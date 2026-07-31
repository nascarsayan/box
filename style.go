package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

var (
	accent = lipgloss.AdaptiveColor{Light: "#8839ef", Dark: "#c6a0f6"}
	muted  = lipgloss.AdaptiveColor{Light: "#6c6f85", Dark: "#939ab7"}
	good   = lipgloss.AdaptiveColor{Light: "#40a02b", Dark: "#a6da95"}
	bad    = lipgloss.AdaptiveColor{Light: "#d20f39", Dark: "#ed8796"}

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	boldStyle   = lipgloss.NewStyle().Bold(true)
	accentStyle = lipgloss.NewStyle().Foreground(accent)
	mutedStyle  = lipgloss.NewStyle().Foreground(muted)
	okStyle     = lipgloss.NewStyle().Bold(true).Foreground(good)
	errStyle    = lipgloss.NewStyle().Bold(true).Foreground(bad)
	boxStyle    = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(0, 2)
)

// Progress output goes to stderr throughout, so that a subcommand whose stdout is
// data -- `box gwq` prints a worktree path the shell cd's into -- stays pipeable.
func step(n, total int, msg string) {
	fmt.Fprintln(os.Stderr, accentStyle.Bold(true).Render(fmt.Sprintf("[%d/%d]", n, total))+" "+msg)
}

func note(msg string) { fmt.Fprintln(os.Stderr, mutedStyle.Render("      "+msg)) }
func okay(msg string) { fmt.Fprintln(os.Stderr, okStyle.Render("  ok  ")+msg) }
func warn(msg string) { fmt.Fprintln(os.Stderr, errStyle.Render(" warn ")+msg) }

func isTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
