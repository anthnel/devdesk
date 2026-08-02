package docker

import (
	"errors"
	"slices"
	"testing"
)

func TestListContainersParsesTabSeparatedOutput(t *testing.T) {
	stubOutput(t, "ps",
		"abc123\tweb\tnginx:1.25\trunning\tUp 2 hours\t2026-08-01 10:00:00 +0200 CEST\t0.0.0.0:80->80/tcp\n"+
			"def456\tdb\tpostgres:16\texited\tExited (0) 5 minutes ago\t2026-07-31 09:00:00 +0200 CEST\t\n")

	got, err := ListContainers(true)

	if err != nil {
		t.Fatalf("ListContainers() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListContainers() returned %d containers, want 2", len(got))
	}
	want := Container{
		ID: "abc123", Name: "web", Image: "nginx:1.25", State: "running",
		Status: "Up 2 hours", CreatedAt: "2026-08-01 10:00:00 +0200 CEST",
		Ports: "0.0.0.0:80->80/tcp",
	}
	if got[0] != want {
		t.Errorf("first container = %+v, want %+v", got[0], want)
	}
	// An empty trailing field must not drop the row or shift the others.
	if got[1].Ports != "" || got[1].Name != "db" {
		t.Errorf("second container = %+v, want db with no ports", got[1])
	}
}

// docker ps emits fewer columns on older engines; a short row must be kept when
// it carries the four fields the UI needs, and skipped when it does not.
func TestListContainersHandlesShortRows(t *testing.T) {
	stubOutput(t, "ps", "abc\tweb\tnginx\trunning\nbroken\trow\n")

	got, err := ListContainers(false)

	if err != nil {
		t.Fatalf("ListContainers() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListContainers() returned %d containers, want 1 (the 2-field row is unusable)", len(got))
	}
	if got[0].Status != "" || got[0].State != "running" {
		t.Errorf("container = %+v, want the four present fields and empty extras", got[0])
	}
}

func TestListContainersPassesAllFlag(t *testing.T) {
	tests := []struct {
		name    string
		all     bool
		wantAll bool
	}{
		{"all=true asks for stopped containers", true, true},
		{"all=false lists running only", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := stubOutput(t, "ps", "")

			if _, err := ListContainers(tt.all); err != nil {
				t.Fatalf("ListContainers() error = %v", err)
			}

			args := s.lastArgs()
			if got := slices.Contains(args, "--all"); got != tt.wantAll {
				t.Errorf("args = %v, --all present = %v, want %v", args, got, tt.wantAll)
			}
			// --format must survive the flag insertion, or parsing breaks.
			if !slices.Contains(args, "--format") {
				t.Errorf("args = %v, missing --format", args)
			}
		})
	}
}

func TestListContainersReportsMissingDocker(t *testing.T) {
	stub(t, &stubRunner{missing: true})

	if _, err := ListContainers(false); err == nil {
		t.Error("ListContainers() returned nil error when docker is absent")
	}
}

func TestGetContainerMetricsParsesPercentagesAndIO(t *testing.T) {
	stubOutput(t, "stats",
		"abc123\t12.34%\t150MiB / 8GiB\t1.83%\t1.2kB / 3.4kB\t5MB / 10MB\n")

	got, err := GetContainerMetrics()

	if err != nil {
		t.Fatalf("GetContainerMetrics() error = %v", err)
	}
	m, ok := got["abc123"]
	if !ok {
		t.Fatalf("GetContainerMetrics() = %v, missing the container ID key", got)
	}
	if m.CPUPercent != 12.34 {
		t.Errorf("CPUPercent = %v, want 12.34", m.CPUPercent)
	}
	if m.MemPercent != 1.83 {
		t.Errorf("MemPercent = %v, want 1.83", m.MemPercent)
	}
	if m.MemUsage != "150MiB / 8GiB" {
		t.Errorf("MemUsage = %q, want it kept verbatim", m.MemUsage)
	}
	if m.NetRX != 1200 || m.NetTX != 3400 {
		t.Errorf("NetRX/NetTX = %d/%d, want 1200/3400", m.NetRX, m.NetTX)
	}
	if m.BlockRX != 5_000_000 || m.BlockTX != 10_000_000 {
		t.Errorf("BlockRX/BlockTX = %d/%d, want 5000000/10000000", m.BlockRX, m.BlockTX)
	}
}

// Engines that omit the BlockIO column still produce usable rows.
func TestGetContainerMetricsToleratesMissingBlockIO(t *testing.T) {
	stubOutput(t, "stats", "abc\t1.0%\t10MiB / 1GiB\t1.0%\t1kB / 2kB\n")

	got, err := GetContainerMetrics()

	if err != nil {
		t.Fatalf("GetContainerMetrics() error = %v", err)
	}
	m := got["abc"]
	if m.BlockIO != "" || m.BlockRX != 0 {
		t.Errorf("BlockIO = %q / %d, want the zero value", m.BlockIO, m.BlockRX)
	}
	if m.NetRX != 1000 {
		t.Errorf("NetRX = %d, want the row still parsed", m.NetRX)
	}
}

func TestContainerLifecycleCommands(t *testing.T) {
	tests := []struct {
		name     string
		call     func() error
		wantArgs []string
	}{
		{"stop", func() error { return StopContainer("abc") }, []string{"stop", "abc"}},
		{"restart", func() error { return RestartContainer("abc") }, []string{"restart", "abc"}},
		{"pause", func() error { return PauseContainer("abc") }, []string{"pause", "abc"}},
		{"unpause", func() error { return UnpauseContainer("abc") }, []string{"unpause", "abc"}},
		{"remove", func() error { return RemoveContainer("abc", false) }, []string{"rm", "abc"}},
		{"remove forced", func() error { return RemoveContainer("abc", true) }, []string{"rm", "-f", "abc"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := stub(t, &stubRunner{})

			if err := tt.call(); err != nil {
				t.Fatalf("call returned error = %v", err)
			}
			if !slices.Equal(s.lastArgs(), tt.wantArgs) {
				t.Errorf("args = %v, want %v", s.lastArgs(), tt.wantArgs)
			}
		})
	}
}

func TestGetContainerLogsBuildsArgs(t *testing.T) {
	tests := []struct {
		name       string
		timestamps bool
		wantArgs   []string
	}{
		{"without timestamps", false, []string{"logs", "--tail", "50", "abc"}},
		{"with timestamps", true, []string{"logs", "--tail", "50", "--timestamps", "abc"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := stubOutput(t, "logs", "line one\nline two\n")

			got, err := GetContainerLogs("abc", 50, tt.timestamps)

			if err != nil {
				t.Fatalf("GetContainerLogs() error = %v", err)
			}
			// Logs are returned verbatim: blank lines are meaningful here.
			if got != "line one\nline two\n" {
				t.Errorf("GetContainerLogs() = %q, want the output untouched", got)
			}
			if !slices.Equal(s.lastArgs(), tt.wantArgs) {
				t.Errorf("args = %v, want %v", s.lastArgs(), tt.wantArgs)
			}
		})
	}
}

func TestFetchContainerStatsCountsByState(t *testing.T) {
	stubOutput(t, "ps", "running\nrunning\npaused\nexited\ncreated\ndead\n")

	got := FetchContainerStats()

	if !got.Available {
		t.Error("Available = false although docker responded")
	}
	if got.Running != 2 {
		t.Errorf("Running = %d, want 2", got.Running)
	}
	if got.Paused != 1 {
		t.Errorf("Paused = %d, want 1", got.Paused)
	}
	// Everything that is neither running nor paused counts as stopped.
	if got.Stopped != 3 {
		t.Errorf("Stopped = %d, want 3 (exited, created, dead)", got.Stopped)
	}
}

// The dashboard renders these counters unconditionally, so a host without
// Docker must produce a zero value flagged unavailable rather than an error.
func TestFetchContainerStatsUnavailable(t *testing.T) {
	tests := []struct {
		name string
		s    *stubRunner
	}{
		{"docker missing", &stubRunner{missing: true}},
		{"docker fails", &stubRunner{err: map[string]error{"ps": errors.New("daemon not running")}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub(t, tt.s)

			got := FetchContainerStats()

			if got.Available {
				t.Error("Available = true although docker could not be queried")
			}
			if got != (ContainerStats{}) {
				t.Errorf("FetchContainerStats() = %+v, want the zero value", got)
			}
		})
	}
}
