package docker

import (
	"reflect"
	"testing"
)

// The forms docker actually emits, each of which broke a naive split at some
// point in the writing of this parser.
func TestParseContainerPortsReadsEveryFormDockerEmits(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []PortBinding
	}{
		{
			name: "published on every v4 interface",
			raw:  "0.0.0.0:80->80/tcp",
			want: []PortBinding{{HostPort: "80", ContainerPort: "80", Protocol: "tcp", Scope: ScopeAll, V4: true}},
		},
		{
			// The address is a bare "::", so the port is behind the *last*
			// colon. Splitting on ":" yields three empty fields and no port.
			name: "the bare v6 wildcard",
			raw:  ":::80->80/tcp",
			want: []PortBinding{{HostPort: "80", ContainerPort: "80", Protocol: "tcp", Scope: ScopeAll, V6: true}},
		},
		{
			name: "the bracketed v6 wildcard",
			raw:  "[::]:80->80/tcp",
			want: []PortBinding{{HostPort: "80", ContainerPort: "80", Protocol: "tcp", Scope: ScopeAll, V6: true}},
		},
		{
			name: "loopback, both spellings",
			raw:  "127.0.0.1:5432->5432/tcp, [::1]:5432->5432/tcp",
			want: []PortBinding{{HostPort: "5432", ContainerPort: "5432", Protocol: "tcp", Scope: ScopeLoopback, V4: true, V6: true}},
		},
		{
			name: "a named address keeps it",
			raw:  "192.168.1.5:8080->80/tcp",
			want: []PortBinding{{HostIP: "192.168.1.5", HostPort: "8080", ContainerPort: "80", Protocol: "tcp", Scope: ScopeAddress, V4: true}},
		},
		{
			name: "a range passes through as docker wrote it",
			raw:  "0.0.0.0:8000-8002->8000-8002/tcp",
			want: []PortBinding{{HostPort: "8000-8002", ContainerPort: "8000-8002", Protocol: "tcp", Scope: ScopeAll, V4: true}},
		},
		{
			// EXPOSE with no publication: there is no host side at all, which is
			// why it must not read like something you can connect to.
			name: "exposed, never published",
			raw:  "6379/tcp",
			want: []PortBinding{{ContainerPort: "6379", Protocol: "tcp", Scope: ScopeExposed}},
		},
		{
			name: "udp keeps its protocol",
			raw:  "0.0.0.0:53->53/udp",
			want: []PortBinding{{HostPort: "53", ContainerPort: "53", Protocol: "udp", Scope: ScopeAll, V4: true}},
		},
		{
			name: "nothing at all",
			raw:  "",
			want: nil,
		},
		{
			name: "a malformed entry is dropped, the rest survives",
			raw:  "garbage, 0.0.0.0:80->80/tcp",
			want: []PortBinding{{HostPort: "80", ContainerPort: "80", Protocol: "tcp", Scope: ScopeAll, V4: true}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseContainerPorts(tc.raw); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseContainerPorts(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}

// The whole point of the merge: docker prints one publication twice, and that
// alone is half the width of the column.
func TestADualStackPublicationIsOneEntry(t *testing.T) {
	got := ParseContainerPorts("0.0.0.0:80->80/tcp, :::80->80/tcp")

	if len(got) != 1 {
		t.Fatalf("got %d entries, want the two halves of one publication merged: %+v", len(got), got)
	}
	if !got[0].V4 || !got[0].V6 {
		t.Errorf("the merged entry claims v4=%v v6=%v, want both", got[0].V4, got[0].V6)
	}
}

// The merge must not swallow publications that differ in anything but family.
func TestTheMergeKeepsGenuinelyDifferentPublicationsApart(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"different scopes", "0.0.0.0:80->80/tcp, 127.0.0.1:80->80/tcp"},
		{"different host ports", "0.0.0.0:80->80/tcp, 0.0.0.0:8080->80/tcp"},
		{"different container ports", "0.0.0.0:80->80/tcp, 0.0.0.0:80->8080/tcp"},
		{"different protocols", "0.0.0.0:53->53/tcp, 0.0.0.0:53->53/udp"},
		{"different named addresses", "10.0.0.1:80->80/tcp, 10.0.0.2:80->80/tcp"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseContainerPorts(tc.raw); len(got) != 2 {
				t.Errorf("got %d entries, want 2: %+v", len(got), got)
			}
		})
	}
}

// Order is content: docker sorts what it prints, and the merge must not reorder
// what survives it.
func TestTheMergePreservesOrder(t *testing.T) {
	got := ParseContainerPorts("0.0.0.0:443->443/tcp, :::443->443/tcp, 0.0.0.0:80->80/tcp")

	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
	if got[0].HostPort != "443" || got[1].HostPort != "80" {
		t.Errorf("order = %s, %s; want the order docker printed", got[0].HostPort, got[1].HostPort)
	}
}
