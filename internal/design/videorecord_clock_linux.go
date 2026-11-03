package design

import "golang.org/x/sys/unix"

// monotonicMs is the browser's clock (CLOCK_MONOTONIC) in ms: a frame's time before it stops the page's CSS animations.
func monotonicMs() (float64, error) {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return 0, err
	}
	return float64(ts.Sec)*1000 + float64(ts.Nsec)/1e6, nil
}
