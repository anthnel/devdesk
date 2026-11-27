package docker

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-09a2 hardening (review of DEVDESK-943): the argv rule reads the options of a
// creation and of a destructive verb against an allow list, refuses the global
// options that point at another engine at any position, and refuses a pod. The
// rule is a pure function and engine-free; the last tests run it inside the
// recorder of a contract, on fakecli, for both engines.

// hardeningRule is the rule of the run labRunID, with a volume, a network and a
// container of the run known to it.
func hardeningRule() (func(argv []string) error, map[string]bool) {
	name := func(role string) string { return "ddc-" + labRunID + "-" + role }
	known := map[string]bool{idA: true, name("c1"): true, name("v1"): true, name("n1"): true}
	return contractArgvRule(labRunID, func(ref string) bool { return known[ref] }), known
}

type ruleCase struct {
	name    string
	argv    string // split on spaces
	wantErr string // substring of the refusal; "" when the argv must be allowed
}

func runRuleCases(t *testing.T, cases []ruleCase) {
	t.Helper()
	rule, _ := hardeningRule()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			argv := strings.Fields(tt.argv)
			err := rule(argv)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("rule(%q) = %v, want it allowed", argv, err)
			case tt.wantErr != "" && err == nil:
				t.Errorf("rule(%q) = nil, want a refusal naming %q", argv, tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Errorf("rule(%q) = %q, want it to name %q", argv, err, tt.wantErr)
			}
		})
	}
}

const (
	ruleLabel  = "devdesk.test=" + labRunID
	ruleName   = "ddc-" + labRunID + "-c1"
	ruleVolume = "ddc-" + labRunID + "-v1"
	ruleNet    = "ddc-" + labRunID + "-n1"
	ruleBase   = "run -d --pull=never --label " + ruleLabel + " "
)

func TestRunRuleAllowsTheOptionsOfTheContractsAndNoOthers(t *testing.T) {
	runRuleCases(t, []ruleCase{
		// Allowed: what the helpers and the contracts send, and their obvious siblings.
		{"the argv of c.container", ruleBase + "--name " + ruleName + " img sleep 600", ""},
		{"--name=", ruleBase + "--name=" + ruleName + " img sleep 1", ""},
		{"--detach and --rm", "run --detach --rm --pull=never --label " + ruleLabel + " img sleep 1", ""},
		{"an explicit environment variable", ruleBase + "-e KEY=value --env=OTHER=1 img sleep 1", ""},
		{"an empty value is explicit", ruleBase + "-e KEY= img sleep 1", ""},
		{"the default bridge", ruleBase + "--network bridge img sleep 1", ""},
		{"no network", ruleBase + "--network=none img sleep 1", ""},
		{"podman's default network", ruleBase + "--net podman img sleep 1", ""},
		{"a network of the run", ruleBase + "--network " + ruleNet + " img sleep 1", ""},
		{"a named volume of the run", ruleBase + "-v " + ruleVolume + ":/data img sleep 1", ""},
		{"a named volume with options", ruleBase + "--volume=" + ruleVolume + ":/data:ro img sleep 1", ""},
		{"entrypoint, user and workdir", ruleBase + "--entrypoint /bin/sh -u 1000 -w /tmp --user=0 --workdir=/ img", ""},
		{"create", "create --pull=never --label " + ruleLabel + " img", ""},
		{"container create", "container create --pull=never --label " + ruleLabel + " --name " + ruleName + " img", ""},
		{"-l is the label", "run -d --pull=never -l " + ruleLabel + " img sleep 1", ""},
		// What follows the image is the container's command, not options of the engine.
		{"the command after the image is not read", ruleBase + "img sleep --privileged -v /:/h", ""},

		// Refused: the name.
		{"a name without the prefix", ruleBase + "--name web img sleep 1", "ddc-" + labRunID + "-"},
		{"the name of another run", ruleBase + "--name ddc-" + foreignRunID + "-c1 img sleep 1", "ddc-" + labRunID + "-"},
		{"the prefix without the run", ruleBase + "--name ddc- img sleep 1", "ddc-" + labRunID + "-"},
		{"--name= without the prefix", ruleBase + "--name=web img sleep 1", "ddc-" + labRunID + "-"},
		{"--replace", ruleBase + "--replace --name " + ruleName + " img sleep 1", "--replace"},
		{"--replace=true", ruleBase + "--replace=true --name " + ruleName + " img sleep 1", "--replace"},

		// Refused: the host (B1).
		{"--privileged", ruleBase + "--privileged img sleep 1", "--privileged"},
		{"--privileged=true", ruleBase + "--privileged=true img sleep 1", "--privileged"},
		{"--mount of a bind", ruleBase + "--mount type=bind,src=/,dst=/h img sleep 1", "--mount"},
		{"--mount=", ruleBase + "--mount=type=bind,src=/etc,dst=/h img sleep 1", "--mount"},
		{"the docker socket", ruleBase + "-v /var/run/docker.sock:/var/run/docker.sock img sleep 1", "/var/run/docker.sock"},
		{"the podman socket", ruleBase + "--volume /run/podman/podman.sock:/run/podman/podman.sock img sleep 1", "/run/podman/podman.sock"},
		{"the docker socket by --volume=", ruleBase + "--volume=/var/run/docker.sock:/s img sleep 1", "/var/run/docker.sock"},
		{"a bind mount of a path", ruleBase + "-v /etc:/h:ro img sleep 1", "/etc"},
		{"a bind mount of the root", ruleBase + "-v /:/host img sleep 1", "/:/host"},
		{"a relative bind mount", ruleBase + "-v ./x:/y img sleep 1", "./x"},
		{"a home bind mount", ruleBase + "-v ~/x:/y img sleep 1", "~/x"},
		{"a named volume that is not of the run", ruleBase + "-v pgdata:/data img sleep 1", "pgdata"},
		{"an anonymous volume", ruleBase + "-v /data img sleep 1", "/data"},
		{"a volume with no target", ruleBase + "-v " + ruleVolume + " img sleep 1", ruleVolume},
		{"--pid=host", ruleBase + "--pid=host img sleep 1", "--pid"},
		{"--pid host", ruleBase + "--pid host img sleep 1", "--pid"},
		{"--ipc host", ruleBase + "--ipc host img sleep 1", "--ipc"},
		{"--uts=host", ruleBase + "--uts=host img sleep 1", "--uts"},
		{"--userns=host", ruleBase + "--userns=host img sleep 1", "--userns"},
		{"--network host", ruleBase + "--network host img sleep 1", "host"},
		{"--net=host", ruleBase + "--net=host img sleep 1", "host"},
		{"the network of another container", ruleBase + "--network container:" + idB + " img sleep 1", "container:"},
		{"a network that is not of the run", ruleBase + "--network backend img sleep 1", "backend"},
		{"--cap-add", ruleBase + "--cap-add ALL img sleep 1", "--cap-add"},
		{"--cap-add=", ruleBase + "--cap-add=SYS_ADMIN img sleep 1", "--cap-add"},
		{"--device", ruleBase + "--device /dev/sda img sleep 1", "--device"},
		{"--security-opt", ruleBase + "--security-opt seccomp=unconfined img sleep 1", "--security-opt"},
		{"--volumes-from", ruleBase + "--volumes-from " + idB + " img sleep 1", "--volumes-from"},
		{"--env-file", ruleBase + "--env-file /etc/shadow img sleep 1", "--env-file"},
		{"a bare environment variable", ruleBase + "-e HOME img sleep 1", "HOME"},
		{"--env with no name", ruleBase + "--env =x img sleep 1", "=x"},

		// Refused: anything the list does not name, with the way to add it.
		{"a published port", ruleBase + "-p 80:80 img sleep 1", "contractArgvRule"},
		{"--publish", ruleBase + "--publish 127.0.0.1:8080:80 img sleep 1", "contractArgvRule"},
		{"a tty", ruleBase + "-t img sleep 1", "contractArgvRule"},
		{"an interactive group", "run -it --pull=never --label " + ruleLabel + " img sh", "contractArgvRule"},
		{"a group with an unknown letter", "run -dl --pull=never --label " + ruleLabel + " img sleep 1", "contractArgvRule"},
		{"an option with its value attached", ruleBase + "-eKEY=v img sleep 1", "contractArgvRule"},
		{"--hostname", ruleBase + "--hostname web img sleep 1", "contractArgvRule"},
		{"an option with no value at the end", "run -d --pull=never --label " + ruleLabel + " --name", "no value"},
		{"container run with --privileged", "container run -d --pull=never --label " + ruleLabel + " --privileged img sleep 1", "--privileged"},
		{"create with --mount", "create --pull=never --label " + ruleLabel + " --mount type=bind,src=/,dst=/h img", "--mount"},

		// Still required.
		{"no label", "run -d --pull=never img sleep 1", "label"},
		{"the label of another run", "run -d --pull=never --label devdesk.test=" + foreignRunID + " img", "label"},
		{"no --pull", "run -d --label " + ruleLabel + " img", "--pull=never"},
		{"--pull=always next to --pull=never", ruleBase + "--pull=always img sleep 1", "--pull=never"},
		{"no image", "run -d --pull=never --label " + ruleLabel, "no image"},
	})
}

func TestNamedCreateRuleTakesOnlyTheLabelAndAName(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"volume create", "volume create --label " + ruleLabel + " " + ruleVolume, ""},
		{"volume create, --label=", "volume create --label=" + ruleLabel + " " + ruleVolume, ""},
		{"network create", "network create --label " + ruleLabel + " " + ruleNet, ""},
		{"volume create with a device option", "volume create --label " + ruleLabel + " --opt device=/etc " + ruleVolume, "contractArgvRule"},
		{"volume create with -o", "volume create --label " + ruleLabel + " -o type=none,o=bind,device=/etc " + ruleVolume, "contractArgvRule"},
		{"volume create with a driver", "volume create --label " + ruleLabel + " --driver local " + ruleVolume, "contractArgvRule"},
		{"network create with a subnet", "network create --label " + ruleLabel + " --subnet 10.0.0.0/8 " + ruleNet, "contractArgvRule"},
		{"network create with --internal", "network create --label " + ruleLabel + " --internal " + ruleNet, "contractArgvRule"},
		{"a volume name without the prefix", "volume create --label " + ruleLabel + " pgdata", "ddc-" + labRunID + "-"},
		{"a network name of another run", "network create --label " + ruleLabel + " ddc-" + foreignRunID + "-n1", "ddc-" + labRunID + "-"},
		{"no name", "volume create --label " + ruleLabel, "name"},
		{"two names", "volume create --label " + ruleLabel + " " + ruleVolume + " " + ruleVolume + "x", "name"},
		{"no label", "network create " + ruleNet, "label"},
	})
}

// No cleanup of pods exists, so a pod would stay in the store after the run.
func TestPodCreateIsRefusedWithAndWithoutTheLabel(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"pod create", "pod create --name " + ruleName, "no cleanup of pods"},
		{"pod create with the label", "pod create --label " + ruleLabel + " --name " + ruleName, "no cleanup of pods"},
		{"pod rm is unknown", "pod rm " + ruleName, "unknown verb"},
	})
}

// A destructive verb takes the options of the verb and no other: -f on a network
// or a volume, --depend, --all and the like change what it reaches.
func TestDestructiveRuleTakesOnlyTheOptionsOfTheVerb(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"rm -f", "rm -f " + idA, ""},
		{"rm -f -v", "rm -f -v " + idA, ""},
		{"rm -fv", "rm -fv " + idA, ""},
		{"rm --force --volumes", "rm --force --volumes " + idA, ""},
		{"rm -t", "rm -f -t 5 " + idA, ""},
		{"rm --time=", "rm --time=5 " + ruleName, ""},
		{"container rm -f", "container rm -f " + idA, ""},
		{"rm --depend", "rm --depend " + idA, "--depend"},
		{"rm --depend after -f", "rm -f --depend " + idA, "--depend"},
		{"rm --all", "rm --all", "--all"},
		{"rm -a", "rm -a", "-a"},
		{"rm -fa", "rm -fa " + idA, "-fa"},
		{"rm --filter", "rm --filter label=x", "--filter"},
		{"rm --cidfile", "rm --cidfile=/tmp/ids " + idA, "--cidfile"},
		{"rm --latest", "rm --latest", "--latest"},
		{"rm --ignore", "rm --ignore " + idA, "--ignore"},
		{"container rm --depend", "container rm --depend " + idA, "--depend"},

		{"network rm", "network rm " + ruleNet, ""},
		{"volume rm", "volume rm " + ruleVolume, ""},
		{"network rm -f", "network rm -f " + ruleNet, "-f"},
		{"network rm --force", "network rm --force " + ruleNet, "--force"},
		{"volume rm -f", "volume rm -f " + ruleVolume, "-f"},
		{"volume rm --force", "volume rm --force " + ruleVolume, "--force"},
		{"volume rm --all", "volume rm --all", "--all"},

		{"stop", "stop " + idA, ""},
		{"stop -t", "stop -t 5 " + idA, ""},
		{"stop --time=", "stop --time=5 " + idA, ""},
		{"stop -s", "stop -s SIGINT " + idA, ""},
		{"kill -s", "kill -s KILL " + idA, ""},
		{"kill --signal=", "kill --signal=KILL " + idA, ""},
		{"stop --all", "stop --all", "--all"},
		{"stop -a", "stop -a", "-a"},
		{"stop --filter", "stop --filter label=x", "--filter"},
		{"stop --ignore", "stop --ignore " + idA, "--ignore"},
		{"kill --all", "kill --all", "--all"},
		{"kill -a", "kill -a", "-a"},
		{"kill --latest", "kill --latest", "--latest"},

		{"restart -t", "restart -t 5 " + idA, ""},
		{"restart --all", "restart --all", "--all"},
		{"start", "start " + idA, ""},
		{"start -a attaches", "start -a " + idA, "-a"},
		{"pause", "pause " + idA, ""},
		{"pause --all", "pause --all", "--all"},
		{"unpause --all", "unpause --all", "--all"},

		{"network connect", "network connect bridge " + idA, ""},
		{"network disconnect", "network disconnect " + ruleNet + " " + idA, ""},
		{"network connect --alias", "network connect --alias web bridge " + idA, "--alias"},
		{"network disconnect -f", "network disconnect -f bridge " + idA, "-f"},
		{"network connect to a stranger", "network connect bridge " + idB, idB},
	})
}

// The options that send the CLI to another engine or store are refused wherever
// they stand, not only in place of the verb (review finding B2).
func TestGlobalEngineOptionsAreRefusedAtAnyPosition(t *testing.T) {
	var cases []ruleCase
	for _, opt := range []string{"--url x", "--url=x", "--connection c", "--connection=c", "--remote", "--root /r",
		"--runroot /r", "--storage-driver vfs", "--storage-driver=vfs", "--host tcp://h:2375", "--host=tcp://h:2375",
		"-H tcp://h:2375", "-Htcp://h:2375", "--context prod", "--context=prod"} {
		name := strings.Fields(opt)[0]
		cases = append(cases,
			ruleCase{"ps " + opt, "ps " + opt, "at any position"},
			ruleCase{"inspect " + opt, "inspect " + opt + " " + idA, name},
			ruleCase{"rm -f " + opt, "rm -f " + opt + " " + idA, name},
			ruleCase{"volume ls " + opt, "volume ls " + opt, name},
			ruleCase{"run, after the options", ruleBase + opt + " img sleep 1", name},
			ruleCase{"run, after the image", ruleBase + "img " + opt, name},
		)
	}
	cases = append(cases,
		ruleCase{"a read that merely has a long name", "ps -a --format {{.Names}} --filter name=hostname", ""},
		ruleCase{"--rootfs is not --root", "inspect --format x --size " + idA, ""},
	)
	runRuleCases(t, cases)
}

// The rule is for the pure function; these run it where it matters, in the recorder
// of a contract, for both engines: an argv it refuses never reaches the engine, one
// it allows does.
func TestTheRecorderRefusesTheDangerousCreationsBeforeTheEngine(t *testing.T) {
	refused := [][]string{
		strings.Fields(ruleBase + "--privileged img sleep 1"),
		strings.Fields(ruleBase + "-v /var/run/docker.sock:/var/run/docker.sock img sleep 1"),
		strings.Fields(ruleBase + "--replace --name " + ruleName + " img sleep 1"),
		strings.Fields(ruleBase + "--pid=host img sleep 1"),
		strings.Fields("ps --host tcp://elsewhere:2375"),
		strings.Fields("network rm --force " + ruleNet),
		strings.Fields("pod create --label " + ruleLabel + " p"),
	}
	allowed := strings.Fields(ruleBase + "--name " + ruleName + " -e A=b --network bridge img sleep 1")
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := fakecli.Install(t, fakecli.Scenario{{Tool: shape.Binary, Args: allowed, Reply: fakecli.Reply{Stdout: idA + "\n"}}}, shape.Binary)
		ct := &contractT{TB: t}
		c := startContract(ct, labSession(enginefixture.Tool(shape.Name), labRunID))

		for _, argv := range refused {
			if _, err := c.rec.Run(dockerCmd{Args: argv, Combined: true}); !errors.Is(err, errNotContracted) {
				t.Errorf("Run(%q) error = %v, want errNotContracted", argv, err)
			}
		}
		if len(ct.errs) != len(refused) {
			t.Errorf("%d reports for %d refusals: %q", len(ct.errs), len(refused), ct.errs)
		}
		if n := len(fake.Invocations()); n != 0 {
			t.Errorf("the engine was started %d time(s) by refused argv: %+v", n, fake.Invocations())
		}
		ct.errs = nil

		out, err := c.rec.Run(dockerCmd{Args: allowed})
		if err != nil || strings.TrimSpace(string(out)) != idA {
			t.Errorf("Run(allowed) = %q, %v, want the id and no error", out, err)
		}
		if got := fake.Invocations(); len(got) != 1 || !slices.Equal(got[0].Args, allowed) {
			t.Errorf("invocations = %+v, want exactly the allowed argv", got)
		}
	})
}

// The image a contract starts is a repository pinned by a digest: nothing that
// moves, nothing an option could be mistaken for (review finding B4).
func TestAContainerNeedsAnImagePinnedByDigest(t *testing.T) {
	sum := strings.Repeat("0", 64)
	bad := []string{
		"alpine", "alpine:latest", "docker.io/library/alpine:3.24", "alpine@sha256:abc",
		"alpine@sha256:" + strings.ToUpper(strings.Repeat("a", 64)), "Alpine@sha256:" + sum,
		"-v/:/h@sha256:" + sum, "alpine@sha256:" + sum + " --privileged", "@sha256:" + sum, "alpine@md5:" + sum,
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, image := range bad {
			t.Run(image, func(t *testing.T) {
				openLab(t)
				t.Setenv(enginecontract.ImageEnv, image)
				q := queued(t) // no step: nothing may reach the engine
				ct := &contractT{TB: t}
				c := newContract(ct, sessionOf(shape, labRunID))

				ended := ct.run(func() { c.container("c1") })

				if !ended || len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], enginecontract.ImageEnv) {
					t.Errorf("container(c1) ended = %v, fatals = %q, want a Fatalf that names %s", ended, ct.fatals, enginecontract.ImageEnv)
				}
				if len(q.calls) != 0 {
					t.Errorf("the engine was reached with the image %q: %v", image, q.calls)
				}
			})
		}
	})
}
