package cmd

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderTableAlignsColumns(t *testing.T) {
	out := renderTable(
		[]string{"NAME", "VALUE"},
		[][]string{{"a", "long value"}, {"bb", "x"}},
	)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4 (header, separator, 2 rows):\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "NAME") || !strings.Contains(lines[0], "VALUE") {
		t.Errorf("header line missing columns: %q", lines[0])
	}
	if !strings.Contains(lines[1], "─") {
		t.Errorf("separator line missing: %q", lines[1])
	}
	// Rows must share one display width (columns aligned), ANSI codes included.
	if w0, w1 := lipgloss.Width(lines[2]), lipgloss.Width(lines[3]); w0 != w1 {
		t.Errorf("row widths differ: %d vs %d\n%s", w0, w1, out)
	}
}

func TestRenderTableEmptyRows(t *testing.T) {
	if out := renderTable([]string{"A"}, nil); out != "" {
		t.Errorf("empty rows should render nothing, got %q", out)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"hello world", 6, "hello…"},
		{"hi", 6, "hi"},
		{"", 3, ""},
		{"héllo wörld", 6, "héllo…"}, // multibyte-safe
	}
	for _, tt := range tests {
		got := truncate(tt.in, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
		if lipgloss.Width(got) > tt.max {
			t.Errorf("truncate(%q, %d) width = %d, exceeds max", tt.in, tt.max, lipgloss.Width(got))
		}
	}
}

func TestPad(t *testing.T) {
	if got := pad("ab", 5); got != "ab   " {
		t.Errorf("pad = %q, want %q", got, "ab   ")
	}
	if got := pad("abcdef", 3); got != "abcdef" {
		t.Errorf("pad must not shrink input, got %q", got)
	}
}
