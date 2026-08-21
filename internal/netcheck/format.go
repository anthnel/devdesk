package netcheck

import (
	"fmt"
	"time"
)

// roundedMillis renders a latency the way a diagnostic reads it: whole
// milliseconds above 1 ms, and a sub-millisecond figure below, because "0 ms"
// on a loopback connection looks like a missing measurement rather than a fast
// one.
func roundedMillis(d time.Duration) string {
	if d <= 0 {
		return "0 ms"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.2f ms", float64(d.Microseconds())/1000)
	}
	return fmt.Sprintf("%d ms", d.Round(time.Millisecond).Milliseconds())
}
