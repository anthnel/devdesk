package nexusbench

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// namedKinds is every kind of the D-F table the tests refer to by name.
var namedKinds = []struct {
	name string
	kind ErrKind
}{
	{"KubectlMissing", KubectlMissing},
	{"ContextNotFound", ContextNotFound},
	{"ClusterUnreachable", ClusterUnreachable},
	{"Forbidden", Forbidden},
	{"NamespaceForeign", NamespaceForeign},
	{"NamespaceMissing", NamespaceMissing},
	{"NodePortTaken", NodePortTaken},
	{"AdmissionDenied", AdmissionDenied},
	{"PodPending", PodPending},
	{"ImagePull", ImagePull},
	{"Crash", Crash},
	{"Deadline", Deadline},
	{"RESTUnreachable", RESTUnreachable},
	{"ProvisionFailed", ProvisionFailed},
}

func TestErrKindsAreDistinct(t *testing.T) {
	seen := map[ErrKind]string{}
	for _, k := range namedKinds {
		if other, dup := seen[k.kind]; dup {
			t.Errorf("%s and %s share the value %d", k.name, other, k.kind)
		}
		seen[k.kind] = k.name
	}
}

func TestOpsAreDistinct(t *testing.T) {
	ops := map[string]Op{"OpUp": OpUp, "OpProvision": OpProvision, "OpDown": OpDown, "OpStatus": OpStatus}
	seen := map[Op]string{}
	for name, op := range ops {
		if other, dup := seen[op]; dup {
			t.Errorf("%s and %s share the value %d", name, other, op)
		}
		seen[op] = name
	}
}

func TestBenchErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *BenchError
		want string
	}{
		{
			name: "cause then check",
			err: &BenchError{
				Kind:  ContextNotFound,
				Cause: `no kubeconfig context named "datomir"`,
				Check: "kubectl config get-contexts",
			},
			want: `no kubeconfig context named "datomir". Check: kubectl config get-contexts`,
		},
		{
			name: "the wrapped error is not part of the message",
			err: &BenchError{
				Kind:  ClusterUnreachable,
				Cause: "the cluster does not answer",
				Check: "kubectl --context datomir get --raw /readyz",
				Err:   errors.New("exit status 1"),
			},
			want: "the cluster does not answer. Check: kubectl --context datomir get --raw /readyz",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A sub-process error can carry a secret in its text; Error() is what reaches
// the terminal, so it must never print the wrapped error.
func TestBenchErrorDoesNotEchoTheWrappedError(t *testing.T) {
	be := &BenchError{
		Kind:  ProvisionFailed,
		Cause: "Nexus refused the request",
		Check: "kubectl get pods",
		Err:   errors.New("exec failed: password=hunter2"),
	}
	if strings.Contains(be.Error(), "hunter2") {
		t.Errorf("Error() echoes the wrapped error: %q", be.Error())
	}
}

func TestBenchErrorUnwrap(t *testing.T) {
	sentinel := errors.New("sentinel")
	with := &BenchError{Kind: Crash, Cause: "c", Check: "k", Err: sentinel}
	if got := with.Unwrap(); got != sentinel {
		t.Errorf("Unwrap() = %v, want the wrapped error", got)
	}
	if !errors.Is(with, sentinel) {
		t.Error("errors.Is does not reach the wrapped error")
	}
	without := &BenchError{Kind: Crash, Cause: "c", Check: "k"}
	if got := without.Unwrap(); got != nil {
		t.Errorf("Unwrap() = %v, want nil when nothing is wrapped", got)
	}
	if errors.Is(without, sentinel) {
		t.Error("errors.Is matched a sentinel that was never wrapped")
	}
}

func TestBenchErrorAsThroughWrapping(t *testing.T) {
	var base error = &BenchError{Kind: Forbidden, Cause: "c", Check: "k"}
	wrapped := fmt.Errorf("preflight: %w", fmt.Errorf("again: %w", base))

	var be *BenchError
	if !errors.As(wrapped, &be) {
		t.Fatal("errors.As does not find the *BenchError")
	}
	if be.Kind != Forbidden {
		t.Errorf("Kind = %d, want Forbidden (%d)", be.Kind, Forbidden)
	}
}

func TestClassifyKubectl(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   ErrKind
	}{
		{
			name:   "connection refused behind Unable to connect",
			stderr: "Unable to connect to the server: dial tcp 192.168.1.31:6443: connect: connection refused\n",
			want:   ClusterUnreachable,
		},
		{
			name:   "TLS handshake timeout behind Unable to connect",
			stderr: "Unable to connect to the server: net/http: TLS handshake timeout\n",
			want:   ClusterUnreachable,
		},
		{
			name:   "i/o timeout behind Unable to connect",
			stderr: "Unable to connect to the server: dial tcp 192.168.1.31:6443: i/o timeout\n",
			want:   ClusterUnreachable,
		},
		{
			name:   "bare connection refused",
			stderr: "dial tcp 127.0.0.1:6443: connect: connection refused\n",
			want:   ClusterUnreachable,
		},
		{
			name:   "forbidden list",
			stderr: "Error from server (Forbidden): services is forbidden: User \"anthoni\" cannot list resource \"services\" in API group \"\" at the cluster scope\n",
			want:   Forbidden,
		},
		{
			name:   "forbidden namespace get",
			stderr: "Error from server (Forbidden): namespaces \"nexus-bench\" is forbidden: User \"anthoni\" cannot get resource \"namespaces\" in API group \"\" in the namespace \"nexus-bench\"\n",
			want:   Forbidden,
		},
		{
			name:   "Forbidden with a capital and nothing else",
			stderr: "Error from server (Forbidden): access refused by the gateway\n",
			want:   Forbidden,
		},
		{
			name:   "NodePort already allocated",
			stderr: "The Service \"nexus\" is invalid: spec.ports[0].nodePort: Invalid value: 30481: provided port is already allocated\n",
			want:   NodePortTaken,
		},
		{
			name:   "NodePort out of range",
			stderr: "The Service \"nexus\" is invalid: spec.ports[0].nodePort: Invalid value: 29999: provided port is not in the valid range. The range of valid ports is 30000-32767\n",
			want:   NodePortTaken,
		},
		{
			name:   "admission webhook denied",
			stderr: "Error from server: admission webhook \"validate.kyverno.svc-fail\" denied the request: resource Deployment/nexus-bench/nexus was blocked due to the following policies\n",
			want:   AdmissionDenied,
		},
		{
			// The more specific reason wins over the generic status.
			name:   "admission webhook denied reported as Forbidden",
			stderr: "Error from server (Forbidden): error when creating \"STDIN\": admission webhook \"x\" denied the request: nope\n",
			want:   AdmissionDenied,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyKubectl(tt.stderr); got != tt.want {
				t.Errorf("classifyKubectl(%q) = %d, want %d", tt.stderr, got, tt.want)
			}
		})
	}
}

// An unknown stderr must not be mistaken for any kind of the table, and the
// zero value of ErrKind must not stand for "classified": the package needs one
// extra value, an "unclassified" kind, that none of the named kinds uses.
func TestClassifyKubectlUnknownIsNoNamedKind(t *testing.T) {
	for _, stderr := range []string{
		"",
		"error: unknown command \"frobnicate\" for \"kubectl\"\n",
		"Error from server (Conflict): Operation cannot be fulfilled on namespaces \"x\": the object has been modified\n",
	} {
		t.Run(stderr, func(t *testing.T) {
			got := classifyKubectl(stderr)
			for _, k := range namedKinds {
				if got == k.kind {
					t.Errorf("classifyKubectl(%q) = %s, but nothing in it says so", stderr, k.name)
				}
			}
		})
	}
}
