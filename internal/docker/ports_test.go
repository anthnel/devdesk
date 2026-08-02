package docker

import (
	"testing"
)

func TestParseSSOutput(t *testing.T) {
	raw := `Netid  State    Recv-Q  Send-Q  Local Address:Port   Peer Address:Port   Process
tcp    LISTEN   0       128     0.0.0.0:22             0.0.0.0:*           users:(("sshd",pid=882,fd=4))
tcp    ESTAB    0       0       192.168.1.5:57204      140.82.113.4:443    users:(("curl",pid=1234,fd=5))
udp    UNCONN   0       0       0.0.0.0:68             0.0.0.0:*           users:(("dhclient",pid=567,fd=5))
tcp    LISTEN   0       128     *:80                   *:*`

	ports := parseSSOutput(raw)
	if len(ports) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(ports))
	}

	// First entry: sshd LISTEN
	p := ports[0]
	if p.Protocol != "tcp" {
		t.Errorf("entry[0] protocol: got %q, want %q", p.Protocol, "tcp")
	}
	if p.State != "LISTEN" {
		t.Errorf("entry[0] state: got %q, want %q", p.State, "LISTEN")
	}
	if p.LocalAddr != "0.0.0.0:22" {
		t.Errorf("entry[0] local: got %q, want %q", p.LocalAddr, "0.0.0.0:22")
	}
	if p.PeerAddr != "0.0.0.0:*" {
		t.Errorf("entry[0] peer: got %q, want %q", p.PeerAddr, "0.0.0.0:*")
	}
	if p.Process != "sshd" {
		t.Errorf("entry[0] process: got %q, want %q", p.Process, "sshd")
	}
	if p.PID != "882" {
		t.Errorf("entry[0] pid: got %q, want %q", p.PID, "882")
	}

	// Second entry: ESTAB curl
	p = ports[1]
	if p.State != "ESTAB" {
		t.Errorf("entry[1] state: got %q, want %q", p.State, "ESTAB")
	}
	if p.Process != "curl" {
		t.Errorf("entry[1] process: got %q, want %q", p.Process, "curl")
	}

	// Third entry: UDP dhclient
	p = ports[2]
	if p.Protocol != "udp" {
		t.Errorf("entry[2] protocol: got %q, want %q", p.Protocol, "udp")
	}
	if p.Process != "dhclient" {
		t.Errorf("entry[2] process: got %q, want %q", p.Process, "dhclient")
	}

	// Fourth entry: no process column
	p = ports[3]
	if p.Process != "" {
		t.Errorf("entry[3] process: got %q, want empty", p.Process)
	}
	if p.PID != "" {
		t.Errorf("entry[3] pid: got %q, want empty", p.PID)
	}
}

// TestParseSSOutputMultiLine covers newer iproute2 that wraps process info to the next line.
func TestParseSSOutputMultiLine(t *testing.T) {
	raw := "Netid  State   Recv-Q  Send-Q  Local Address:Port   Peer Address:Port\n" +
		"tcp    LISTEN  0       128     0.0.0.0:22             0.0.0.0:*          \n" +
		"\t\tusers:((\"sshd\",pid=882,fd=4))\n" +
		"udp    UNCONN  0       0       0.0.0.0:68             0.0.0.0:*          \n" +
		"\t\tusers:((\"dhclient\",pid=567,fd=5))\n"

	ports := parseSSOutput(raw)
	if len(ports) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(ports))
	}
	if ports[0].Process != "sshd" || ports[0].PID != "882" {
		t.Errorf("entry[0]: got process=%q pid=%q, want sshd/882", ports[0].Process, ports[0].PID)
	}
	if ports[1].Process != "dhclient" || ports[1].PID != "567" {
		t.Errorf("entry[1]: got process=%q pid=%q, want dhclient/567", ports[1].Process, ports[1].PID)
	}
}

func TestParseSSOutputEmpty(t *testing.T) {
	ports := parseSSOutput("")
	if len(ports) != 0 {
		t.Errorf("expected 0 entries for empty input, got %d", len(ports))
	}
}

func TestParseSSOutputHeaderOnly(t *testing.T) {
	ports := parseSSOutput("Netid  State    Recv-Q  Send-Q  Local Address:Port   Peer Address:Port   Process")
	if len(ports) != 0 {
		t.Errorf("expected 0 entries for header-only input, got %d", len(ports))
	}
}

func TestParseSSProcess(t *testing.T) {
	tests := []struct {
		input string
		name  string
		pid   string
	}{
		{`users:(("sshd",pid=882,fd=4))`, "sshd", "882"},
		{`users:(("nginx",pid=1234,fd=5),("nginx",pid=1235,fd=5))`, "nginx", "1234"},
		{`users:(("python3",pid=99,fd=3))`, "python3", "99"},
		{``, "", ""},
		{`users:(())`, "", ""},
	}
	for _, tt := range tests {
		name, pid := parseSSProcess(tt.input)
		if name != tt.name || pid != tt.pid {
			t.Errorf("parseSSProcess(%q) = (%q, %q), want (%q, %q)",
				tt.input, name, pid, tt.name, tt.pid)
		}
	}
}
