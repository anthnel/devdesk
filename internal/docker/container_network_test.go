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
	t.Run("podman", func(t *testing.T) {
		s := stub(t, &stubRunner{})
		useEngine(t, engine.Podman)
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
	}})

	_, _ = InspectContainerNetworking(ctx, testCtrID)
	_, _ = EngineOperatingSystem(ctx)
	_, _ = ListContainersContext(ctx, false)

	if len(s.calls) != 3 {
		t.Fatalf("calls = %v, want 3", allArgs(s))
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
	}})
	ctx := context.Background()

	_, _ = InspectContainerNetworking(ctx, testCtrID)
	_, _ = EngineOperatingSystem(ctx)
	_, _ = ListContainersContext(ctx, false)

	if len(s.calls) != 3 {
		t.Fatalf("calls = %v, want the three reads", allArgs(s))
	}
	forbidden := []string{"start", "run", "exec", "create", "restart", "unpause", "attach", "kill", "stop"}
	for _, c := range s.calls {
		switch c.Args[0] {
		case "ps", "info":
		case "container":
			if len(c.Args) < 2 || c.Args[1] != "inspect" {
				t.Errorf("container call %v is not inspect", c.Args)
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
