package ociresources

import (
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-15 (DEVDESK-813): the launch form refuses a reserved environment name
// (`DOCKER_HOST`, `HOME`, `PATH`, `LD_*`, `*_PROXY`, ...) because the typed
// entries also land in the environment of the engine CLI. The rule is
// docker.ReservedEnvName, shared with the launcher. Like every Environment
// error (OC-14, Rule 140) the field is underlined after the first refused
// submit, the footer says it at level Error with a fixed sentence that never
// repeats the name or the value, and the error follows the edits.
//
// Expected production behavior, pinned below: validate() returns the "env"
// FieldError for a reserved name, with a text containing reservedEnvFooter.

const reservedEnvFooter = "cannot be set from this form"

var reservedFormInputs = []struct {
	name  string
	input string
}{
	{"DOCKER_HOST", "DOCKER_HOST=tcp://other:2375"},
	{"mixed case", "Docker_Host=tcp://other:2375"},
	{"lower case", "docker_host=tcp://other:2375"},
	{"CONTAINER_HOST", "CONTAINER_HOST=ssh://root@other/run/podman.sock"},
	{"DOCKER_CONFIG", "DOCKER_CONFIG=/tmp/cfg"},
	{"REGISTRY_AUTH_FILE", "REGISTRY_AUTH_FILE=/tmp/auth.json"},
	{"DOCKER_TLS", "DOCKER_TLS=1"},
	{"DOCKER_CUSTOM_HEADERS", "DOCKER_CUSTOM_HEADERS=X-Evil=1"},
	{"CONTAINER_SSHKEY", "CONTAINER_SSHKEY=/tmp/key"},
	{"PODMAN_CONNECTIONS_CONF", "PODMAN_CONNECTIONS_CONF=/tmp/c.json"},
	{"CONTAINERS_CONF_OVERRIDE", "CONTAINERS_CONF_OVERRIDE=/tmp/o.conf"},
	{"XDG_DATA_HOME", "XDG_DATA_HOME=/tmp/data"},
	{"USERPROFILE", "USERPROFILE=C:\\x"},
	{"STORAGE_OPTS", "STORAGE_OPTS=x"},
	{"SSL_CERT_FILE", "SSL_CERT_FILE=/tmp/ca.pem"},
	{"GODEBUG", "GODEBUG=x=1"},
	{"TMPDIR", "TMPDIR=/tmp/x"},
	{"DOCKER_CONTENT_TRUST_SERVER", "DOCKER_CONTENT_TRUST_SERVER=https://evil"},
	{"the OTEL_ prefix", "OTEL_EXPORTER_OTLP_ENDPOINT=http://evil"},
	{"the _CONTAINERS_ prefix", "_CONTAINERS_X=1"},
	{"HOME", "HOME=/tmp/home"},
	{"PATH", "PATH=/tmp/bin"},
	{"SSH_AUTH_SOCK", "SSH_AUTH_SOCK=/tmp/agent"},
	{"LD_PRELOAD", "LD_PRELOAD=/tmp/evil.so"},
	{"DYLD_INSERT_LIBRARIES", "DYLD_INSERT_LIBRARIES=/tmp/evil.dylib"},
	{"HTTP_PROXY", "HTTP_PROXY=http://proxy.example:3128"},
	{"lower case https_proxy", "https_proxy=http://proxy.example:3128"},
	{"a custom proxy name", "MY_PROXY=x"},
	{"NO_PROXY", "NO_PROXY=*"},
	{"among valid entries", "LOG_LEVEL=info, DOCKER_HOST=tcp://other:2375, REGION=eu"},
	{"a reserved name with an empty value", "HOME="},
}

// Criterion 1: refused at the submit, underlined (4:3), named in the footer at
// level Error, nothing submitted so nothing is launched.
func TestLaunchFormRefusesAReservedEnvironmentName(t *testing.T) {
	for _, tt := range reservedFormInputs {
		t.Run(tt.name, func(t *testing.T) {
			f := newEnvForm()
			setEnv(f, tt.input)

			focusLaunchField(f, f.fieldSubmit())
			_, cmd := f.Update(testutil.Key("enter"))
			if cmd != nil {
				t.Fatalf("a refused submit returned a command: nothing may start for Environment %q", tt.input)
			}
			if input, rendered := envUnderlined(f); !input || !rendered {
				t.Errorf("Environment %q: underlined = input %v, form %v, want both", tt.input, input, rendered)
			}
			if strings.Contains(f.nameInput.View(), curlyUnderline) {
				t.Error("the Name field is underlined, only Environment is faulty")
			}
			st := footerStatus(f)
			if st.Level != sharedcomponents.LevelError {
				t.Fatalf("footer level = %v, want Error (text %q)", st.Level, st.Text)
			}
			if !strings.Contains(st.Text, reservedEnvFooter) {
				t.Errorf("footer %q does not say the name %s", st.Text, reservedEnvFooter)
			}
			for _, leak := range []string{tt.input, "tcp://", "proxy.example", "/tmp", "evil"} {
				if strings.Contains(st.Text, leak) {
					t.Errorf("footer %q repeats part of what was typed (%q)", st.Text, leak)
				}
			}
			if name, _, _ := strings.Cut(tt.input, "="); strings.Contains(st.Text, name) {
				t.Errorf("footer %q names the typed variable %q", st.Text, name)
			}
		})
	}
}

// An entry that is merely invalid keeps the OC-14 sentence; the reserved
// sentence is for a reserved name only.
func TestLaunchFormKeepsTheGenericSentenceForAnEntryThatIsNotReserved(t *testing.T) {
	f := newEnvForm()
	setEnv(f, "API_TOKEN")

	if _, ok := submitOpts(f); ok {
		t.Fatal("a bare name was accepted")
	}
	if st := footerStatus(f); st.Text == "" || strings.Contains(st.Text, reservedEnvFooter) {
		t.Errorf("footer = %q, want the generic Environment sentence", st.Text)
	}
}

// Criterion 2: an exact match only. These look reserved and are not.
func TestLaunchFormAcceptsANameThatOnlyLooksReserved(t *testing.T) {
	for _, name := range []string{"DOCKER_HOSTNAME", "MY_PATH", "PATHS", "HOMEPAGE", "CONTAINERS_X_Y", "LDAP_URL", "PROXY", "MY_PROXYX"} {
		t.Run(name, func(t *testing.T) {
			f := newEnvForm()
			setEnv(f, name+"=value")

			opts, ok := submitOpts(f)

			if !ok {
				t.Fatalf("Environment %s=value was refused: %q", name, footerStatus(f).Text)
			}
			if !slices.Equal(opts.Env, []string{name + "=value"}) {
				t.Errorf("Opts.Env = %q, want the typed variable", opts.Env)
			}
			if input, rendered := envUnderlined(f); input || rendered {
				t.Error("an accepted Environment is underlined")
			}
		})
	}
}

// The error follows the edits like any Environment error: nothing before the
// first refused submit, then it appears and disappears with the text.
func TestLaunchFormReservedNameErrorFollowsEditsAfterARefusal(t *testing.T) {
	f := newEnvForm()
	setEnv(f, "DOCKER_HOST=tcp://other:2375")
	if _, ok := submitOpts(f); ok {
		t.Fatal("precondition: a reserved name must be refused")
	}
	if !strings.Contains(footerStatus(f).Text, reservedEnvFooter) {
		t.Fatal("precondition: the refused submit must set the reserved-name footer")
	}

	// Editing the name into one that is not reserved clears the error: the
	// keystroke revalidates the whole form.
	focusLaunchField(f, f.fieldEnv())
	setEnv(f, "DOCKER_HOSTNAME=tcp://other:2375")
	f.envInput.CursorEnd()
	f, _ = f.Update(testutil.Key("x")) // any edit revalidates the whole form

	if input, rendered := envUnderlined(f); input || rendered {
		t.Error("Environment is still underlined after the name stopped being reserved")
	}
	if st := footerStatus(f); st.Text != "" {
		t.Errorf("footer = %q after the correction, want empty", st.Text)
	}

	// Revalidation stays live: a reserved name typed again shows at once.
	f.envInput.SetValue("")
	f.envInput.CursorEnd()
	for _, m := range testutil.Type("HOME=/tmp") {
		f, _ = f.Update(m)
	}
	if input, _ := envUnderlined(f); !input {
		t.Error("a reserved name typed after a refusal was not underlined at once")
	}
	if st := footerStatus(f); st.Level != sharedcomponents.LevelError || !strings.Contains(st.Text, reservedEnvFooter) {
		t.Errorf("footer = {%q, %v}, want the reserved-name Error", st.Text, st.Level)
	}

	setEnv(f, "HOME_DIR=/tmp")
	if _, ok := submitOpts(f); !ok {
		t.Errorf("submit refused after the correction: %q", footerStatus(f).Text)
	}
}

// A malformed entry is not called reserved: `PATH` with no "=", or a name that
// only reads as a reserved one once case-folded (a long s, U+017F, folds to S) is
// refused with the generic sentence. Both are still refused and underlined.
func TestLaunchFormDoesNotCallAMalformedEntryReserved(t *testing.T) {
	for _, input := range []string{"PATH", "DOCKER_HOST", "docker_ho\u017ft=x", "DOCKER_HOST =x", "=PATH"} {
		t.Run(input, func(t *testing.T) {
			f := newEnvForm()
			setEnv(f, input)

			if _, ok := submitOpts(f); ok {
				t.Fatalf("Environment %q was accepted", input)
			}
			if in, _ := envUnderlined(f); !in {
				t.Error("the field is not underlined")
			}
			if st := footerStatus(f); st.Level != sharedcomponents.LevelError || st.Text == "" || strings.Contains(st.Text, reservedEnvFooter) {
				t.Errorf("footer = {%q, %v}, want the generic Environment sentence", st.Text, st.Level)
			}
		})
	}
}

// Nothing is flagged before the first refused submit (OC-14 criterion 7): a
// reserved name restored from the cache is refused at the submit only, however
// the form is edited first.
func TestLaunchFormReservedNameIsNotFlaggedBeforeTheFirstRefusedSubmit(t *testing.T) {
	m := launchingModel(t, "80/tcp")
	m = feed(t, m, LaunchOptionsCacheLoadedMsg{
		ImageName: "api:v1",
		Entry:     &cache.LaunchOptionsEntry{EnvNames: []string{"DOCKER_HOST"}, Detach: true},
	})
	f := m.launchForm
	if got := f.envInput.Value(); got != "DOCKER_HOST=" {
		t.Fatalf("Environment = %q, want the cached reserved entry", got)
	}
	focusLaunchField(f, f.fieldEnv())
	f.envInput.CursorEnd()
	f, _ = f.Update(testutil.Key("0")) // a keystroke, no submit yet

	if input, rendered := envUnderlined(f); input || rendered {
		t.Error("the cached reserved name is underlined before any submit")
	}
	if st := footerStatus(f); st.Text != "" {
		t.Errorf("footer = %q before any submit, want empty", st.Text)
	}

	// The first submit refuses it.
	if _, ok := submitOpts(f); ok {
		t.Fatal("the submit went through with a reserved name")
	}
	if st := footerStatus(f); !strings.Contains(st.Text, reservedEnvFooter) {
		t.Errorf("footer = %q after the refused submit, want the reserved-name sentence", st.Text)
	}
}
