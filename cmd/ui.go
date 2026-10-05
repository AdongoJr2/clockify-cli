package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// Brand colors for the CLI design system.
const (
	colorAccent  = "#03A9F4" // Clockify cyan/blue — headers & highlights
	colorSuccess = "#4CAF50" // success badges
	colorError   = "#F44336" // error badges
	colorWarn    = "#FF9800" // warnings
	colorMuted   = "#9E9E9E" // secondary text
)

var (
	accentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorSuccess)).Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colorError)).Bold(true)
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colorWarn)).Bold(true)
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	headerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Bold(true)
)

// banner returns the styled app banner shown in `clockify --help`.
func banner() string {
	title := accentStyle.Render("⏱  clockify") + "\n"
	sub := mutedStyle.Render("Start/stop timers, log time, and list entries — right from your terminal.")
	return title + sub
}

// printSuccess prints a green ✓ success line to stdout.
func printSuccess(format string, a ...any) {
	fmt.Println(successStyle.Render("✓") + " " + fmt.Sprintf(format, a...))
}

// printWarn prints an orange ! warning line to stdout.
func printWarn(format string, a ...any) {
	fmt.Println(warnStyle.Render("!") + " " + fmt.Sprintf(format, a...))
}

// printError prints a red ✗ error line to stderr.
func printError(err error) {
	fmt.Fprintln(os.Stderr, errorStyle.Render("✗")+" "+err.Error())
}

// isInteractive reports whether stdin and stdout are terminals, i.e. whether
// Huh forms can run. Non-interactive invocations must supply flags instead.
func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// nonEmpty returns a Huh validator rejecting blank input.
func nonEmpty(field string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
}

// renderTable renders an aligned table with a styled header and separator.
// It returns an empty string when there are no rows.
func renderTable(headers []string, rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = lipgloss.Width(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && lipgloss.Width(cell) > widths[i] {
				widths[i] = lipgloss.Width(cell)
			}
		}
	}

	var b strings.Builder

	head := make([]string, len(headers))
	for i, h := range headers {
		head[i] = headerStyle.Render(pad(h, widths[i]))
	}
	b.WriteString(strings.Join(head, "  ") + "\n")

	sep := make([]string, len(headers))
	for i, w := range widths {
		sep[i] = mutedStyle.Render(strings.Repeat("─", w))
	}
	b.WriteString(strings.Join(sep, "  ") + "\n")

	for _, row := range rows {
		cells := make([]string, len(headers))
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			cells[i] = pad(cell, widths[i])
		}
		b.WriteString(strings.Join(cells, "  ") + "\n")
	}
	return b.String()
}

// pad right-aligns s to width w with spaces (by display width).
func pad(s string, w int) string {
	if diff := w - lipgloss.Width(s); diff > 0 {
		return s + strings.Repeat(" ", diff)
	}
	return s
}

// truncate shortens s to at most n display cells, appending an ellipsis.
func truncate(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	runes := []rune(s)
	kept := make([]rune, 0, len(runes))
	width := 0
	for _, r := range runes {
		w := lipgloss.Width(string(r))
		if width+w > n-1 {
			break
		}
		kept = append(kept, r)
		width += w
	}
	return string(kept) + "…"
}
