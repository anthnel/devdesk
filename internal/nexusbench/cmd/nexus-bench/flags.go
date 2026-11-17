package main

import (
	"flag"
	"fmt"
	"io"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/nexusbench"
)

// Defaults of the flags that are not a NEXUS_BENCH_* parameter.
const (
	loopbackHost = "127.0.0.1"
	maxHostLen   = 253
)

var (
	// hostName is a DNS name: letters, digits, '.' and '-', no port, no path,
	// no credentials, no whitespace.
	hostName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)
	// ipv6Zone is the zone of a link-local address ("eth0" in fe80::1%eth0).
	ipv6Zone = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// flagKind is the type of a flag, which decides how it is registered and how
// its value is handed to nexusbench.FromEnv.
type flagKind int

const (
	kindString flagKind = iota
	kindInt
	kindDuration
	kindBool
	// kindConsent is a boolean whose true is the exact word "yes", the only
	// value nexusbench accepts for NEXUS_BENCH_ACCEPT_EULA.
	kindConsent
)

// paramFlag is one flag that stands for one NEXUS_BENCH_* variable.
type paramFlag struct {
	name  string
	env   string
	kind  flagKind
	usage string
}

// commonFlags are accepted by every subcommand.
func commonFlags() []paramFlag {
	d := nexusbench.Defaults()
	return []paramFlag{
		{"context", "NEXUS_BENCH_CONTEXT", kindString, "kubectl context of the cluster (default " + d.Context + ")"},
		{"namespace", "NEXUS_BENCH_NAMESPACE", kindString, "namespace of the bench, which down deletes (default " + d.Namespace + ")"},
		{"http-nodeport", "NEXUS_BENCH_HTTP_NODEPORT", kindInt, fmt.Sprintf("NodePort of the REST API and the UI (default %d)", d.HTTPNodePort)},
		{"docker-nodeport", "NEXUS_BENCH_DOCKER_NODEPORT", kindInt, fmt.Sprintf("NodePort of the Docker connector (default %d)", d.DockerNodePort)},
		{"cpu-request", "NEXUS_BENCH_CPU_REQUEST", kindString, "CPU request (default " + d.CPURequest + ")"},
		{"cpu-limit", "NEXUS_BENCH_CPU_LIMIT", kindString, "CPU limit (default " + d.CPULimit + ")"},
		{"memory-request", "NEXUS_BENCH_MEMORY_REQUEST", kindString, "memory request (default " + d.MemoryRequest + ")"},
		{"memory-limit", "NEXUS_BENCH_MEMORY_LIMIT", kindString, "memory limit (default " + d.MemoryLimit + ")"},
		{"storage-size", "NEXUS_BENCH_STORAGE_SIZE", kindString, "size of the data volume (default " + d.StorageSize + ")"},
		{"jvm-opts", "NEXUS_BENCH_JVM_OPTS", kindString, "JVM options of Nexus (default " + d.JVMOpts + ")"},
		{"image", "NEXUS_BENCH_IMAGE", kindString, "Nexus image, without the tag (default " + d.Image + ")"},
		{"image-tag", "NEXUS_BENCH_IMAGE_TAG", kindString, "Nexus image tag (default " + d.ImageTag + ")"},
		{"upstream-image", "NEXUS_BENCH_UPSTREAM_IMAGE", kindString, "image of the bench upstream registry, without the tag (default " + d.UpstreamImage + ")"},
		{"upstream-image-tag", "NEXUS_BENCH_UPSTREAM_IMAGE_TAG", kindString, "tag of the bench upstream registry image (default " + d.UpstreamImageTag + ")"},
		{"upstream-url", "NEXUS_BENCH_UPSTREAM_URL", kindString, "upstream of the proxy repository (default http://bench-upstream.<namespace>.svc.cluster.local:5000)"},
		{"anonymous", "NEXUS_BENCH_ANONYMOUS", kindBool, "enable anonymous access, --anonymous=false to disable (default true)"},
		{"hosts", "NEXUS_BENCH_HOSTS", kindString, "comma-separated hosts the NodePorts are tried on (default 127.0.0.1 and the node address)"},
		{"timeout", "NEXUS_BENCH_TIMEOUT", kindDuration, "how long up waits for Nexus, and how long the seed may run (default " + d.Timeout.String() + ")"},
		{"delete-timeout", "NEXUS_BENCH_DELETE_TIMEOUT", kindDuration, "how long down waits for the namespace to go (default " + d.DeleteTimeout.String() + ")"},
		{"secrets", "NEXUS_BENCH_SECRETS", kindString, "secrets file, outside the repository (default under $XDG_STATE_HOME/devdesk/nexus-bench)"},
		{"descriptor", "NEXUS_BENCH_DESCRIPTOR", kindString, "descriptor file (default internal/nexusbench/descriptor.json)"},
	}
}

// provisionFlags are accepted by the subcommands that provision Nexus.
func provisionFlags() []paramFlag {
	return []paramFlag{
		{"accept-eula", "NEXUS_BENCH_ACCEPT_EULA", kindConsent, "accept the Nexus Community Edition EULA on your behalf: read it first (default false; the variable counts only as the exact value yes)"},
	}
}

// restHostFlag names the flag that picks the host the admin credentials are sent to.
const restHostFlag = "rest-host"

// flagSet is the flags of one subcommand.
type flagSet struct {
	fs       *flag.FlagSet
	specs    map[string]paramFlag
	restHost *string
}

// newFlagSet registers the flags of a subcommand. It prints nothing by itself:
// the caller reports a parse error on stderr and the help on stdout.
func newFlagSet(name string) *flagSet {
	// up and provision provision Nexus (EULA); they and seed send the admin
	// credentials to the REST API (REST host).
	provisions := name == "up" || name == "provision"
	reachesREST := provisions || name == "seed"
	fs := flag.NewFlagSet("nexus-bench "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	set := &flagSet{fs: fs, specs: map[string]paramFlag{}}
	specs := commonFlags()
	if provisions {
		specs = append(specs, provisionFlags()...)
	}
	for _, spec := range specs {
		set.specs[spec.name] = spec
		switch spec.kind {
		case kindString:
			fs.String(spec.name, "", spec.usage)
		case kindInt:
			fs.Int(spec.name, 0, spec.usage)
		case kindDuration:
			fs.Duration(spec.name, time.Duration(0), spec.usage)
		case kindBool, kindConsent:
			fs.Bool(spec.name, false, spec.usage)
		}
	}
	switch {
	case name == "seed":
		// seed refuses another host (main.go): its credentials and its images
		// always go through the loopback.
		set.restHost = fs.String(restHostFlag, "", "loopback address the REST API is reached on (default "+loopbackHost+"); seed accepts a loopback address only, because its images and the admin credentials always go through "+loopbackHost)
	case reachesREST:
		set.restHost = fs.String(restHostFlag, "", "host the REST API is reached on, and so the host the admin credentials are sent to (default "+loopbackHost+"); another host sends them in clear text. It concerns the REST API only: the images always go through "+loopbackHost)
	}
	return set
}

// parse reads args. It returns flag.ErrHelp for -h and --help, and an error for
// an unknown flag, a value of the wrong type or an argument that is not a flag.
func (s *flagSet) parse(args []string) error {
	if err := s.fs.Parse(args); err != nil {
		return err
	}
	if s.fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", s.fs.Arg(0))
	}
	return nil
}

// printHelp writes the flags and their default to w.
func (s *flagSet) printHelp(w io.Writer) {
	s.fs.SetOutput(w)
	s.fs.PrintDefaults()
	s.fs.SetOutput(io.Discard)
}

// overlay returns getenv with every flag that was given laid over its
// NEXUS_BENCH_* variable: nexusbench.FromEnv then applies "flag, then variable,
// then default" in one place, with the same parsing and the same follow-the-
// namespace rule for the upstream URL.
func (s *flagSet) overlay(getenv func(string) string) func(string) string {
	given := map[string]string{}
	s.fs.Visit(func(f *flag.Flag) {
		spec, ok := s.specs[f.Name]
		if !ok {
			return
		}
		value := f.Value.String()
		if spec.kind == kindConsent {
			value = map[bool]string{true: "yes", false: "no"}[value == "true"]
		}
		given[spec.env] = value
	})
	return func(name string) string {
		if v, ok := given[name]; ok {
			return v
		}
		return getenv(name)
	}
}

// restHostGiven returns the --rest-host value and whether it was set.
func (s *flagSet) restHostGiven() (string, bool) {
	if s.restHost == nil {
		return "", false
	}
	given := false
	s.fs.Visit(func(f *flag.Flag) {
		if f.Name == restHostFlag {
			given = true
		}
	})
	return *s.restHost, given
}

// resolveParams returns the validated parameters of a run: defaults, then the
// NEXUS_BENCH_* variables, then the flags.
func (s *flagSet) resolveParams(getenv func(string) string) (nexusbench.Params, error) {
	p, err := nexusbench.FromEnv(nexusbench.Defaults(), s.overlay(getenv))
	if err != nil {
		return p, err
	}
	return p, p.Validate()
}

// validateRESTHost refuses what is not a host name or an IP address: a port, a
// path, a scheme, credentials or whitespace would change where the admin
// credentials go.
func validateRESTHost(host string) error {
	if host == "" || len(host) > maxHostLen {
		return fmt.Errorf("--%s: %q is not a host name or an IP address", restHostFlag, host)
	}
	if addr, ok := hostAddress(host); ok {
		if zone := addr.Zone(); zone != "" && !ipv6Zone.MatchString(zone) {
			return fmt.Errorf("--%s: %q has an unusable IPv6 zone", restHostFlag, host)
		}
		return nil
	}
	if !hostName.MatchString(host) {
		return fmt.Errorf("--%s: %q is not a host name or an IP address (no port, scheme, path or credentials)", restHostFlag, host)
	}
	return nil
}

// hostAddress parses host as an IP address, with or without the brackets of an
// IPv6 literal.
func hostAddress(host string) (netip.Addr, bool) {
	if len(host) > 1 && strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	addr, err := netip.ParseAddr(host)
	return addr, err == nil
}

// isLoopbackHost tells whether host stays on this machine.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, ok := hostAddress(host)
	return ok && addr.Unmap().IsLoopback()
}
