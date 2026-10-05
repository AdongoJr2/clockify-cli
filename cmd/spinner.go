package cmd

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// spinnerFrames is the braille animation drawn while an API request runs.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const (
	spinnerDelay   = 150 * time.Millisecond // don't flash on fast responses
	spinnerTick    = 100 * time.Millisecond
	spinnerCleanup = "\r\x1b[2K" // carriage return + clear line
)

// runWithSpinner runs fn while showing an animated spinner on stderr, and
// returns fn's error unchanged.
//
// Behavior:
//   - Only animates when stderr is a TTY; otherwise fn runs untouched, so
//     pipes, CI, and tests never see escape codes.
//   - Nothing is drawn for the first 150ms, so fast responses don't flash.
//   - The line is erased before returning, so success/error output is clean.
//
// Never wrap interactive huh forms with this — only API request sections.
func runWithSpinner(title string, fn func() error) error {
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return fn()
	}

	var (
		stop    = make(chan struct{})
		stopped = make(chan struct{})
		drawn   atomic.Bool
		once    sync.Once
	)
	stopSpinner := func() {
		once.Do(func() {
			close(stop)
			<-stopped
			if drawn.Load() {
				fmt.Fprint(os.Stderr, spinnerCleanup)
			}
		})
	}
	defer stopSpinner()

	go func() {
		defer close(stopped)

		// Wait before the first frame so quick calls stay silent.
		delay := time.NewTimer(spinnerDelay)
		defer delay.Stop()
		select {
		case <-stop:
			return
		case <-delay.C:
		}

		style := lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent))
		ticker := time.NewTicker(spinnerTick)
		defer ticker.Stop()
		for i := 0; ; i++ {
			drawn.Store(true)
			fmt.Fprintf(os.Stderr, "\r%s %s", style.Render(spinnerFrames[i%len(spinnerFrames)]), title)
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()

	err := fn()
	stopSpinner()
	return err
}
