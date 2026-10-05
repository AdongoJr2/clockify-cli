package cmd

import (
	"errors"
	"testing"
)

// Under `go test` stderr is never a TTY, so runWithSpinner must take its
// no-animation fast path: run fn untouched and pass its error through.
func TestRunWithSpinnerPassthroughWhenStderrNotTTY(t *testing.T) {
	sentinel := errors.New("sentinel error")
	err := runWithSpinner("working…", func() error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel error propagated", err)
	}

	if err := runWithSpinner("working…", func() error { return nil }); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

// The spinner must never break the wrapped function's work or swallow output.
func TestRunWithSpinnerExecutesFunctionOnce(t *testing.T) {
	calls := 0
	if err := runWithSpinner("working…", func() error { calls++; return nil }); err != nil {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 {
		t.Errorf("fn called %d times, want 1", calls)
	}
}
