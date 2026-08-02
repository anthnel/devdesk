package docker

import (
	"slices"
	"testing"
)

func TestListNetworksParsesOutput(t *testing.T) {
	stubOutput(t, "network",
		"abc123\tbridge\tbridge\tlocal\t2026-08-01 10:00:00 +0200 CEST\n"+
			"def456\thost\thost\tlocal\t2026-07-01 10:00:00 +0200 CEST\n")

	got, err := ListNetworks()

	if err != nil {
		t.Fatalf("ListNetworks() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListNetworks() returned %d networks, want 2", len(got))
	}
	want := Network{ID: "abc123", Name: "bridge", Driver: "bridge", Scope: "local", Created: "2026-08-01 10:00:00 +0200 CEST"}
	if got[0] != want {
		t.Errorf("first network = %+v, want %+v", got[0], want)
	}
}

func TestCreateNetworkDefaultsToBridge(t *testing.T) {
	tests := []struct {
		name       string
		driver     string
		wantDriver string
	}{
		{"explicit driver is used", "overlay", "overlay"},
		{"empty driver falls back to bridge", "", "bridge"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := stub(t, &stubRunner{})

			if err := CreateNetwork("mynet", tt.driver); err != nil {
				t.Fatalf("CreateNetwork() error = %v", err)
			}

			args := s.lastArgs()
			idx := slices.Index(args, "--driver")
			if idx < 0 || idx+1 >= len(args) {
				t.Fatalf("args = %v, missing --driver", args)
			}
			if args[idx+1] != tt.wantDriver {
				t.Errorf("driver = %q, want %q", args[idx+1], tt.wantDriver)
			}
		})
	}
}

func TestParseNetworkInspect(t *testing.T) {
	const output = `[{"Containers":{
		"cid2":{"Name":"web","MacAddress":"02:42:ac:11:00:03","IPv4Address":"172.17.0.3/16"},
		"cid1":{"Name":"db","MacAddress":"02:42:ac:11:00:02","IPv4Address":"172.17.0.2/16"}
	}}]`

	got, err := parseNetworkInspect([]byte(output))

	if err != nil {
		t.Fatalf("parseNetworkInspect() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parseNetworkInspect() returned %d containers, want 2", len(got))
	}
	// Map iteration order is random, so the result is sorted by name to keep
	// the table from reshuffling between polls.
	if got[0].Name != "db" || got[1].Name != "web" {
		t.Errorf("names = %q, %q, want them sorted (db, web)", got[0].Name, got[1].Name)
	}
	// The CIDR mask is display noise for a per-container address column.
	if got[0].IPv4 != "172.17.0.2" {
		t.Errorf("IPv4 = %q, want the mask stripped", got[0].IPv4)
	}
	if got[0].MacAddress != "02:42:ac:11:00:02" {
		t.Errorf("MacAddress = %q, want it preserved", got[0].MacAddress)
	}
}

func TestParseNetworkInspectEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantLen int
		wantErr bool
	}{
		{"empty array yields nothing", `[]`, 0, false},
		{"network with no containers", `[{"Containers":{}}]`, 0, false},
		{"malformed JSON is an error", `not json`, 0, true},
		{"address without a mask is kept", `[{"Containers":{"c":{"Name":"a","IPv4Address":"10.0.0.1"}}}]`, 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseNetworkInspect([]byte(tt.output))

			if (err != nil) != tt.wantErr {
				t.Fatalf("parseNetworkInspect() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(got) != tt.wantLen {
				t.Errorf("parseNetworkInspect() returned %d containers, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestRunDiagnosticContainerAttachesToNetwork(t *testing.T) {
	s := stubOutput(t, "run", "PING 172.17.0.2: 56 data bytes\n")

	got, err := RunDiagnosticContainer("mynet", "nicolaka/netshoot", []string{"ping", "-c", "1", "172.17.0.2"})

	if err != nil {
		t.Fatalf("RunDiagnosticContainer() error = %v", err)
	}
	if got != "PING 172.17.0.2: 56 data bytes" {
		t.Errorf("output = %q, want it trimmed", got)
	}

	args := s.lastArgs()
	idx := slices.Index(args, "--network")
	if idx < 0 || args[idx+1] != "mynet" {
		t.Errorf("args = %v, want --network mynet", args)
	}
	if !slices.Contains(args, "--rm") {
		t.Errorf("args = %v, missing --rm: the probe container would be left behind", args)
	}
	// The command must follow the image, or docker reads it as its own flags.
	imageIdx := slices.Index(args, "nicolaka/netshoot")
	if imageIdx < 0 || imageIdx > slices.Index(args, "ping") {
		t.Errorf("args = %v, want the command after the image", args)
	}
}

// A failing diagnostic still carries useful output (partial traceroute, error
// text), so the caller gets both the output and the error.
func TestRunDiagnosticContainerReturnsOutputOnFailure(t *testing.T) {
	stub(t, &stubRunner{
		output: map[string][]byte{"run": []byte("ping: bad address 'nope'\n")},
		err:    map[string]error{"run": errExit},
	})

	got, err := RunDiagnosticContainer("mynet", "img", []string{"ping", "nope"})

	if err == nil {
		t.Error("RunDiagnosticContainer() error = nil, want the failure reported")
	}
	if got != "ping: bad address 'nope'" {
		t.Errorf("output = %q, want the diagnostic returned alongside the error", got)
	}
}
