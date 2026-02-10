package ui

import (
	"fmt"
	"sync"
	"time"
)

// Spinner shows an animated wave indicator for long-running operations.
// The animation uses block element characters rendered as a 3-bar equalizer.
type Spinner struct {
	done chan struct{}
	once sync.Once
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

// NewSpinner starts an animated spinner with the given message.
// Call Stop() to clear the spinner line and release resources.
func NewSpinner(message string) *Spinner {
	s := &Spinner{
		done: make(chan struct{}),
	}

	go func() {
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		frame := 0
		for {
			select {
			case <-s.done:
				fmt.Print("\r\033[K")
				return
			case <-ticker.C:
				fmt.Printf("\r\033[K  %s %s", Cyan(waveFrames[frame]), Dim(message))
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
		// Brief sleep to let the goroutine clear the line
		time.Sleep(10 * time.Millisecond)
	})
}
