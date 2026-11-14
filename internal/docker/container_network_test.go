package docker

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

const testCtrID = "0123456789ab"

func TestParseContainerNetworking(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
		check   func(t *testing.T, n ContainerNetworking)
	}{
		{
			name: "one bridge endpoint",
			in:   `{"Name":"/web-1","Mode":"bridge","Running":true,"Networks":{"bridge":{"IPAddress":"172.17.0.3","Gateway":"172.17.0.1","GwPriority":0,"IPPrefixLen":16}}}`,
			check: func(t *testing.T, n ContainerNetworking) {
				if n.Name != "web-1" || n.Mode != "bridge" || !n.Running || len(n.Networks) != 1 {
					t.Fatalf("got %+v", n)
				}
				e := n.Networks[0]
				if e.Name != "bridge" || !e.IPv4.Is4() || e.IPv4.String() != "172.17.0.3" ||
					!e.Gateway.Is4() || e.Gateway.String() != "172.17.0.1" {
					t.Errorf("endpoint = %+v", e)
				}
				if e.GwPriority == nil || *e.GwPriority != 0 {
					t.Errorf("GwPriority = %v, want pointer to 0", e.GwPriority)
				}
			},
		},
		{
			name: "GwPriority absent is nil and networks sort by name",
			in:   `{"Name":"/x","Mode":"z","Running":false,"Networks":{"z":{"IPAddress":"10.0.0.2","Gateway":"10.0.0.1"},"a":{"IPAddress":"10.1.0.2","Gateway":"10.1.0.1"}}}`,
			check: func(t *testing.T, n ContainerNetworking) {
				if len(n.Networks) != 2 || n.Networks[0].Name != "a" || n.Networks[1].Name != "z" {
					t.Fatalf("Networks = %+v, want a then z", n.Networks)
				}
				for _, e := range n.Networks {
					if e.GwPriority != nil {
						t.Errorf("%s GwPriority = %v, want nil", e.Name, *e.GwPriority)
					}
				}
				if n.Running {
					t.Error("Running = true, want false")
				}
			},
		},
		{
			name: "host mode has no address",
			in:   `{"Name":"/h","Mode":"host","Running":true,"Networks":{"host":{"IPAddress":""}}}`,
			check: func(t *testing.T, n ContainerNetworking) {
				if n.Mode != "host" || len(n.Networks) != 1 {
					t.Fatalf("got %+v", n)
				}
				if n.Networks[0].IPv4.IsValid() || n.Networks[0].Gateway.IsValid() {
					t.Errorf("addresses should be zero: %+v", n.Networks[0])
				}
			},
		},
		{
			name: "garbage address and IPv6 gateway become zero without error",
			in:   `{"Name":"/g","Mode":"bridge","Running":true,"Networks":{"n":{"IPAddress":"not-an-ip","Gateway":"fe80::1"}}}`,
			check: func(t *testing.T, n ContainerNetworking) {
				if len(n.Networks) != 1 || n.Networks[0].IPv4.IsValid() || n.Networks[0].Gateway.IsValid() {
					t.Errorf("got %+v", n.Networks)
				}
			},
		},
		{name: "invalid JSON", in: `{"Name":`, wantErr: true},
		{name: "error text mixed with JSON", in: "Error: boom\n" + `{"Name":"/a","Mode":"bridge","Running":true,"Networks":{}}`, wantErr: true},
		{name: "oversize output", in: `{"Name":"` + strings.Repeat("a", 257<<10) + `"}`, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, err := parseContainerNetworking([]byte(tc.in))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tc.check(t, n)
		})
	}
}

func TestContainerNetworkingFormatSelectsNoSecrets(t *testing.T) {
	for _, bad := range []string{"Env", "Labels", "Mounts", ".Config", "Cmd"} {
		if strings.Contains(containerNetworkingFormat, bad) {
			t.Errorf("format mentions %s", bad)
		}
	}
	for _, want := range []string{".Name", ".HostConfig.NetworkMode", ".State.Running", ".NetworkSettings.Networks"} {
		if !strings.Contains(containerNetworkingFormat, want) {
			t.Errorf("format lacks %s", want)
		}
	}
}

func TestInspectContainerNetworkingArgs(t *testing.T) {
	s := stubOutput(t, "container", `{"Name":"/web","Mode":"bridge","Running":true,"Networks":{}}`)
	ctx := context.Background()

	n, err := InspectContainerNetworking(ctx, testCtrID)

	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if n.Name != "web" {
		t.Errorf("Name = %q, want web", n.Name)
	}
	want := []string{"container", "inspect", "--format", containerNetworkingFormat, testCtrID}
	if len(s.calls) != 1 || !slices.Equal(s.calls[0].Args, want) {
		t.Fatalf("calls = %v, want one %v", allArgs(s), want)
	}
	if s.calls[0].Ctx == nil {
		t.Error("Ctx is nil")
	}
	if s.calls[0].Combined {
		t.Error("Combined = true: stderr would be mixed into the JSON")
	}
}

func TestInspectContainerNetworkingErrorsCarryNoOutput(t *testing.T) {
	stub(t, &stubRunner{
		output: map[string][]byte{"container": []byte("SECRET-OUTPUT")},
		err:    map[string]error{"container": errExit},
	})

	_, err := InspectContainerNetworking(context.Background(), testCtrID)

	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "SECRET-OUTPUT") {
		t.Errorf("error leaks output: %v", err)
	}
	if !errors.Is(err, errExit) {
		t.Errorf("error does not wrap the cause: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "docker container inspect failed") {
		t.Errorf("error = %q", err)
	}
}

func TestEngineReadsRefuseBeforeRunning(t *testing.T) {
	ctx := context.Background()
	for _, id := range []string{"web-1", "--help", ""} {
		t.Run("id "+id, func(t *testing.T) {
			s := stubOutput(t, "container", "{}")
			if _, err := InspectContainerNetworking(ctx, id); err == nil {
				t.Error("want error for invalid id")
			}
			if len(s.calls) != 0 {
				t.Errorf("calls = %v, want none", allArgs(s))
			}
		})
	}
	t.Run("network id invalid", func(t *testing.T) {
		s := stubOutput(t, "network", testBridgeJSON("bridge", "", testNetID))
		for _, bad := range []string{"bridge", "-f", "my-net", "", "../x", testNetID[:12]} {
			if _, err := InspectNetworkBridge(ctx, bad); err == nil {
				t.Errorf("id %q: want error", bad)
			}
		}
		if len(s.calls) != 0 {
			t.Errorf("calls = %v, want none", allArgs(s))
		}
	})
	t.Run("podman", func(t *testing.T) {
		s := stub(t, &stubRunner{})
		useEngine(t, engine.Podman)
		if _, err := InspectNetworkBridge(ctx, testNetID); !errors.Is(err, ErrDockerOnly) {
			t.Errorf("network err = %v, want ErrDockerOnly", err)
		}
		if _, err := InspectContainerNetworking(ctx, testCtrID); !errors.Is(err, ErrDockerOnly) {
			t.Errorf("inspect err = %v, want ErrDockerOnly", err)
		}
		if _, err := EngineOperatingSystem(ctx); !errors.Is(err, ErrDockerOnly) {
			t.Errorf("os err = %v, want ErrDockerOnly", err)
		}
		if len(s.calls) != 0 {
			t.Errorf("calls = %v, want none", allArgs(s))
		}
	})
	t.Run("engine absent", func(t *testing.T) {
		s := stub(t, &stubRunner{missing: true})
		if _, err := InspectNetworkBridge(ctx, testNetID); err == nil {
			t.Error("network: want error")
		}
		if _, err := InspectContainerNetworking(ctx, testCtrID); err == nil {
			t.Error("inspect: want error")
		}
		if _, err := EngineOperatingSystem(ctx); err == nil {
			t.Error("os: want error")
		}
		if _, err := ListContainersContext(ctx, false); err == nil {
			t.Error("ps: want error")
		}
		if len(s.calls) != 0 {
			t.Errorf("calls = %v, want none", allArgs(s))
		}
	})
}

func TestEngineOperatingSystem(t *testing.T) {
	tests := []struct{ name, out, want string }{
		{"desktop", "Docker Desktop\n", "Docker Desktop"},
		{"debian", "  Debian GNU/Linux 13 (trixie) \n", "Debian GNU/Linux 13 (trixie)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := stubOutput(t, "info", tc.out)
			got, err := EngineOperatingSystem(context.Background())
			if err != nil || got != tc.want {
				t.Errorf("got %q, %v; want %q", got, err, tc.want)
			}
			want := []string{"info", "--format", "{{.OperatingSystem}}"}
			if len(s.calls) != 1 || !slices.Equal(s.calls[0].Args, want) {
				t.Fatalf("calls = %v, want %v", allArgs(s), want)
			}
			if s.calls[0].Ctx == nil || s.calls[0].Combined {
				t.Errorf("Ctx nil or Combined set: %+v", s.calls[0])
			}
		})
	}
	t.Run("failure", func(t *testing.T) {
		stub(t, &stubRunner{err: map[string]error{"info": errExit}})
		if _, err := EngineOperatingSystem(context.Background()); !errors.Is(err, errExit) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestListContainersContextArgs(t *testing.T) {
	const row = "0123456789ab\tweb-1\tnginx\trunning\tUp 1 hour\t1 hour ago\t\n"
	for _, all := range []bool{false, true} {
		s := stubOutput(t, "ps", row)
		got, err := ListContainersContext(context.Background(), all)
		if err != nil || len(got) != 1 || got[0].Name != "web-1" {
			t.Fatalf("all=%v got %+v, %v", all, got, err)
		}
		want := []string{"ps", "--format", templates().PS}
		if all {
			want = []string{"ps", "--all", "--format", templates().PS}
		}
		if len(s.calls) != 1 || !slices.Equal(s.calls[0].Args, want) {
			t.Errorf("all=%v calls = %v, want %v", all, allArgs(s), want)
		}
		if s.calls[0].Ctx == nil {
			t.Errorf("all=%v Ctx is nil", all)
		}
	}
}

func TestEngineReadsHonourCancellation(t *testing.T) {
	// The stub ignores ctx, so the contract checked here is that the very
	// context given reaches the runner, where exec.CommandContext kills it.
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "mine")
	s := stub(t, &stubRunner{output: map[string][]byte{
		"container": []byte(`{"Name":"/a","Mode":"bridge","Running":true,"Networks":{}}`),
		"info":      []byte("x"),
		"ps":        []byte(""),
		"network":   []byte(testBridgeJSON("bridge", "", testNetID)),
	}})

	_, _ = InspectContainerNetworking(ctx, testCtrID)
	_, _ = EngineOperatingSystem(ctx)
	_, _ = ListContainersContext(ctx, false)
	_, _ = InspectNetworkBridge(ctx, testNetID)

	if len(s.calls) != 4 {
		t.Fatalf("calls = %v, want 4", allArgs(s))
	}
	for _, c := range s.calls {
		if c.Ctx == nil || c.Ctx.Value(ctxKey{}) != "mine" {
			t.Errorf("%v did not receive the caller's context", c.Args)
		}
	}
}

// Criterion 7, engine level: nothing a Path-tab read does can start or alter a
// container.
func TestPathSourceReadsStartNothing(t *testing.T) {
	s := stub(t, &stubRunner{output: map[string][]byte{
		"container": []byte(`{"Name":"/a","Mode":"bridge","Running":true,"Networks":{}}`),
		"info":      []byte("Docker Desktop"),
		"ps":        []byte(""),
		"network":   []byte(testBridgeJSON("bridge", "", testNetID)),
	}})
	ctx := context.Background()

	_, _ = InspectContainerNetworking(ctx, testCtrID)
	_, _ = EngineOperatingSystem(ctx)
	_, _ = ListContainersContext(ctx, false)
	_, _ = InspectNetworkBridge(ctx, testNetID)

	if len(s.calls) != 4 {
		t.Fatalf("calls = %v, want the four reads", allArgs(s))
	}
	forbidden := []string{"start", "run", "exec", "create", "restart", "unpause", "attach", "kill", "stop"}
	for _, c := range s.calls {
		switch c.Args[0] {
		case "ps", "info":
		case "container":
			if len(c.Args) < 2 || c.Args[1] != "inspect" {
				t.Errorf("container call %v is not inspect", c.Args)
			}
		case "network":
			if len(c.Args) < 2 || c.Args[1] != "inspect" {
				t.Errorf("network call %v is not inspect", c.Args)
			}
		default:
			t.Errorf("unexpected subcommand %v", c.Args)
		}
		for _, a := range c.Args {
			if slices.Contains(forbidden, a) {
				t.Errorf("forbidden arg %q in %v", a, c.Args)
			}
		}
	}
}

// testNetID is a full 64-hex network ID, as the engine reports it.
const testNetID = "9f3c1a7be2d44c5f8a6b0e1d2c3b4a5968778695a4b3c2d1e0f9a8b7c6d5e4f3"

// testBridgeJSON builds the output InspectNetworkBridge expects from the
// engine. If the format changes, only this helper changes.
func testBridgeJSON(driver, option, id string) string {
	return `{"Driver":"` + driver + `","Name":"` + option + `","Id":"` + id + `"}`
}

func TestParseContainerNetworkingReadsNetworkID(t *testing.T) {
	tests := []struct{ name, id, want string }{
		{"hex id kept", testNetID, testNetID},
		{"short hex becomes empty", testNetID[:12], ""},
		{"not hex becomes empty", "NOT-HEX; rm -rf", ""},
		{"absent becomes empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := `{"Name":"/a","Mode":"bridge","Running":true,"Networks":{"bridge":{"NetworkID":"` + tc.id + `","IPAddress":"172.17.0.2","Gateway":"172.17.0.1"}}}`
			n, err := parseContainerNetworking([]byte(in))
			if err != nil || len(n.Networks) != 1 {
				t.Fatalf("got %+v, %v", n, err)
			}
			if got := n.Networks[0].NetworkID; got != tc.want {
				t.Errorf("NetworkID = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBridgeNameFor(t *testing.T) {
	tests := []struct {
		name, driver, option, id, want string
	}{
		{"default bridge option", "bridge", "docker0", testNetID, "docker0"},
		{"custom option", "bridge", "mybr0", testNetID, "mybr0"},
		{"no option derives from id", "bridge", "", testNetID, "br-9f3c1a7be2d4"},
		{"no option and short id", "bridge", "", "9f3c1a7be2d", ""},
		{"no option and non-hex id", "bridge", "", strings.Repeat("zz", 32), ""},
		{"no option and empty id", "bridge", "", "", ""},
		{"macvlan", "macvlan", "eth0", testNetID, ""},
		{"ipvlan", "ipvlan", "", testNetID, ""},
		{"overlay", "overlay", "", testNetID, ""},
		{"null", "null", "", testNetID, ""},
		{"host", "host", "", testNetID, ""},
		{"empty driver", "", "docker0", testNetID, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := bridgeNameFor(tc.driver, tc.option, tc.id); got != tc.want {
				t.Errorf("bridgeNameFor(%q, %q, %q) = %q, want %q", tc.driver, tc.option, tc.id, got, tc.want)
			}
		})
	}
}

func TestParseNetworkBridge(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    NetworkBridge
		wantErr bool
	}{
		{"option docker0", testBridgeJSON("bridge", "docker0", testNetID), NetworkBridge{Driver: "bridge", Name: "docker0"}, false},
		{"option mybr0", testBridgeJSON("bridge", "mybr0", testNetID), NetworkBridge{Driver: "bridge", Name: "mybr0"}, false},
		{"derived from id", testBridgeJSON("bridge", "", testNetID), NetworkBridge{Driver: "bridge", Name: "br-9f3c1a7be2d4"}, false},
		{"bridge with bad id and no option", testBridgeJSON("bridge", "", "xyz"), NetworkBridge{Driver: "bridge"}, false},
		{"macvlan has no name", testBridgeJSON("macvlan", "", testNetID), NetworkBridge{Driver: "macvlan"}, false},
		{"overlay has no name", testBridgeJSON("overlay", "", testNetID), NetworkBridge{Driver: "overlay"}, false},
		{"host has no name", testBridgeJSON("host", "", testNetID), NetworkBridge{Driver: "host"}, false},
		{"null has no name", testBridgeJSON("null", "", testNetID), NetworkBridge{Driver: "null"}, false},
		{"not JSON", "Error: no such network", NetworkBridge{}, true},
		{"truncated JSON", `{"Driver":`, NetworkBridge{}, true},
		{"empty output", "", NetworkBridge{}, true},
		{"oversize output", testBridgeJSON("bridge", strings.Repeat("a", 257<<10), testNetID), NetworkBridge{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseNetworkBridge([]byte(tc.in))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestInspectNetworkBridgeArgs(t *testing.T) {
	s := stubOutput(t, "network", testBridgeJSON("bridge", "docker0", testNetID))

	got, err := InspectNetworkBridge(context.Background(), testNetID)

	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got != (NetworkBridge{Driver: "bridge", Name: "docker0"}) {
		t.Errorf("got %+v", got)
	}
	if len(s.calls) != 1 {
		t.Fatalf("calls = %v, want one", allArgs(s))
	}
	c := s.calls[0]
	// The ID is the last, single argv element (never a name).
	if len(c.Args) != 5 || c.Args[0] != "network" || c.Args[1] != "inspect" ||
		c.Args[2] != "--format" || c.Args[4] != testNetID {
		t.Errorf("args = %v, want network inspect --format <fmt> %s", c.Args, testNetID)
	} else if !strings.Contains(c.Args[3], "com.docker.network.bridge.name") {
		t.Errorf("format %q does not read com.docker.network.bridge.name", c.Args[3])
	}
	if c.Ctx == nil {
		t.Error("Ctx is nil")
	}
	if c.Combined {
		t.Error("Combined = true: stderr would be mixed into the JSON")
	}
}

func TestInspectNetworkBridgeRefusesBadIDsBeforeRunning(t *testing.T) {
	for _, id := range []string{"bridge", "-f", "--help", "my-net", "ZZ", ""} {
		t.Run("id "+id, func(t *testing.T) {
			s := stubOutput(t, "network", testBridgeJSON("bridge", "docker0", testNetID))
			if _, err := InspectNetworkBridge(context.Background(), id); err == nil {
				t.Error("want error")
			}
			if len(s.calls) != 0 {
				t.Errorf("calls = %v, want none", allArgs(s))
			}
		})
	}
}

func TestInspectNetworkBridgeErrorsCarryNoOutput(t *testing.T) {
	stub(t, &stubRunner{
		output: map[string][]byte{"network": []byte("SECRET-OUTPUT")},
		err:    map[string]error{"network": errExit},
	})

	_, err := InspectNetworkBridge(context.Background(), testNetID)

	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "SECRET-OUTPUT") {
		t.Errorf("error leaks output: %v", err)
	}
	if !errors.Is(err, errExit) {
		t.Errorf("error does not wrap the cause: %v", err)
	}
}

func TestInspectNetworkBridgeBadOutputIsAnError(t *testing.T) {
	tests := []struct{ name, out string }{
		{"not JSON", "SECRET-OUTPUT not json"},
		{"oversize", testBridgeJSON("bridge", strings.Repeat("a", 257<<10), testNetID)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubOutput(t, "network", tc.out)
			_, err := InspectNetworkBridge(context.Background(), testNetID)
			if err == nil {
				t.Fatal("want error")
			}
			if strings.Contains(err.Error(), "SECRET-OUTPUT") {
				t.Errorf("error leaks output: %v", err)
			}
		})
	}
}

func TestInspectNetworkBridgeHonoursCancellation(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "mine")
	s := stubOutput(t, "network", testBridgeJSON("bridge", "docker0", testNetID))

	_, _ = InspectNetworkBridge(ctx, testNetID)

	if len(s.calls) != 1 || s.calls[0].Ctx == nil || s.calls[0].Ctx.Value(ctxKey{}) != "mine" {
		t.Errorf("the caller's context did not reach the runner: %v", allArgs(s))
	}
}
