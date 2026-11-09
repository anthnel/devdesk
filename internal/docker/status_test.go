package docker

import (
	"testing"
	"time"
)

const day = 24 * time.Hour

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   StatusFacts
	}{
		// docker: "Up X"
		{"up hours", "Up 3 hours", StatusFacts{Age: 3 * time.Hour, HasAge: true}},
		{"up paused", "Up 3 hours (Paused)", StatusFacts{Age: 3 * time.Hour, HasAge: true}},
		{"up healthy", "Up 3 hours (healthy)", StatusFacts{Age: 3 * time.Hour, HasAge: true}},
		{"up unhealthy", "Up 3 hours (unhealthy)", StatusFacts{Age: 3 * time.Hour, HasAge: true}},
		{"up health starting", "Up 2 minutes (health: starting)", StatusFacts{Age: 2 * time.Minute, HasAge: true}},
		{"up less than a second", "Up Less than a second", StatusFacts{Age: 0, HasAge: true}},
		{"up seconds", "Up 45 seconds", StatusFacts{Age: 45 * time.Second, HasAge: true}},
		{"up about a minute", "Up About a minute", StatusFacts{Age: time.Minute, HasAge: true}},
		{"up about an hour", "Up About an hour", StatusFacts{Age: time.Hour, HasAge: true}},
		{"up days", "Up 2 days", StatusFacts{Age: 48 * time.Hour, HasAge: true}},
		{"up weeks", "Up 3 weeks", StatusFacts{Age: 21 * day, HasAge: true}},
		{"up months", "Up 4 months", StatusFacts{Age: 120 * day, HasAge: true}},
		{"up years", "Up 2 years", StatusFacts{Age: 730 * day, HasAge: true}},
		// podman may add a trailing "ago"
		{"podman up ago", "Up 3 hours ago", StatusFacts{Age: 3 * time.Hour, HasAge: true}},
		{"podman exited ago", "Exited (0) 3 hours ago", StatusFacts{HasExitCode: true, Age: 3 * time.Hour, HasAge: true}},
		{"podman up less than a second ago", "Up Less than a second ago", StatusFacts{Age: 0, HasAge: true}},
		{"podman up about a minute ago", "Up About a minute ago", StatusFacts{Age: time.Minute, HasAge: true}},
		// exited / restarting
		{"exited 137", "Exited (137) 5 minutes ago", StatusFacts{ExitCode: 137, HasExitCode: true, Age: 5 * time.Minute, HasAge: true}},
		{"exited 0 about an hour", "Exited (0) About an hour ago", StatusFacts{ExitCode: 0, HasExitCode: true, Age: time.Hour, HasAge: true}},
		{"exited 2", "Exited (2) 1 minute ago", StatusFacts{ExitCode: 2, HasExitCode: true, Age: time.Minute, HasAge: true}},
		{"exited negative code", "Exited (-1) 5 minutes ago", StatusFacts{ExitCode: -1, HasExitCode: true, Age: 5 * time.Minute, HasAge: true}},
		{"restarting", "Restarting (1) 3 seconds ago", StatusFacts{ExitCode: 1, HasExitCode: true, Age: 3 * time.Second, HasAge: true}},
		// nothing invented
		{"created", "Created", StatusFacts{}},
		{"dead", "Dead", StatusFacts{}},
		{"removal in progress", "Removal In Progress", StatusFacts{}},
		{"empty", "", StatusFacts{}},
		{"exit code not a number", "Exited (x) 5 minutes ago", StatusFacts{}},
		{"up forever", "Up forever", StatusFacts{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseStatus(tt.status); got != tt.want {
				t.Errorf("ParseStatus(%q) = %+v, want %+v", tt.status, got, tt.want)
			}
		})
	}
}

func TestListContainersFillsFacts(t *testing.T) {
	stubOutput(t, "ps",
		"abc123\tjob\tbusybox\texited\tExited (2) 1 minute ago\t2026-07-31 09:00:00 +0200 CEST\t\n")

	got, err := ListContainers(true)
	if err != nil {
		t.Fatalf("ListContainers() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d containers, want 1", len(got))
	}
	want := StatusFacts{ExitCode: 2, HasExitCode: true, Age: time.Minute, HasAge: true}
	if got[0].Facts != want {
		t.Errorf("Facts = %+v, want %+v", got[0].Facts, want)
	}
}
