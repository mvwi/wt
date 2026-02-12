package ui

import (
	"fmt"
	"sync"
	"time"
)

// Spinner shows an animated wave indicator for long-running operations.
// The animation uses block element characters rendered as a 3-bar equalizer.
type Spinner struct {
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

// Wave frames: 3 bars that rise and fall in sequence.
var waveFrames = []string{
	"▃ ▁ ▃",
	"▅ ▂ ▁",
	"▇ ▅ ▂",
	"█ ▇ ▅",
	"▇ █ ▇",
	"▅ ▇ █",
	"▂ ▅ ▇",
	"▁ ▂ ▅",
}

// NewSpinner starts an animated spinner with the given message (auto-dimmed).
// Call Stop() to clear the spinner line and release resources.
func NewSpinner(message string) *Spinner {
	return newSpinner(Dim(message))
}

// NewSpinnerRich starts a spinner that renders the message as-is, without
// wrapping it in Dim. Use this when the message contains its own ANSI colors.
func NewSpinnerRich(message string) *Spinner {
	return newSpinner(message)
}

func newSpinner(formatted string) *Spinner {
	s := &Spinner{
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}

	go func() {
		defer close(s.stopped)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		frame := 0
		for {
			select {
			case <-s.done:
				fmt.Print("\r\033[K")
				return
			case <-ticker.C:
				fmt.Printf("\r\033[K  %s %s", Cyan(waveFrames[frame]), formatted)
				frame = (frame + 1) % len(waveFrames)
			}
		}
	}()

	return s
}

// Stop clears the spinner line and joins the goroutine.
// Safe to call multiple times.
func (s *Spinner) Stop() {
	s.once.Do(func() {
		close(s.done)
		<-s.stopped
	})
}
