package theme

import (
	"testing"
	"time"
)

func TestAgoFor(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "now"},
		{30 * time.Second, "now"},
		{59 * time.Minute, "59 min ago"},
		{3 * time.Hour, "3 hr ago"},
		{24 * time.Hour, "1 day ago"},
		{45 * 24 * time.Hour, "1 mo ago"},
		{400 * 24 * time.Hour, "1 yr ago"},
	}
	for _, tt := range tests {
		if got := AgoFor(tt.d); got != tt.want {
			t.Errorf("AgoFor(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestTimeAgoUsesAgoFor(t *testing.T) {
	if got := TimeAgo(time.Time{}); got != "" {
		t.Errorf("TimeAgo(zero) = %q, want empty", got)
	}
	for _, d := range []time.Duration{10 * time.Second, 5 * time.Minute, 7 * time.Hour, 3 * 24 * time.Hour, 90 * 24 * time.Hour} {
		if got, want := TimeAgo(time.Now().Add(-d)), AgoFor(d); got != want {
			t.Errorf("TimeAgo(now-%v) = %q, AgoFor = %q", d, got, want)
		}
	}
}
