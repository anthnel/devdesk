package nexusbench

import (
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Environment variables read by FromEnv. An empty value counts as unset.
const (
	envContext        = "NEXUS_BENCH_CONTEXT"
	envNamespace      = "NEXUS_BENCH_NAMESPACE"
	envHTTPNodePort   = "NEXUS_BENCH_HTTP_NODEPORT"
	envDockerNodePort = "NEXUS_BENCH_DOCKER_NODEPORT"
	envCPURequest     = "NEXUS_BENCH_CPU_REQUEST"
	envCPULimit       = "NEXUS_BENCH_CPU_LIMIT"
	envMemoryRequest  = "NEXUS_BENCH_MEMORY_REQUEST"
	envMemoryLimit    = "NEXUS_BENCH_MEMORY_LIMIT"
	envStorageSize    = "NEXUS_BENCH_STORAGE_SIZE"
	envJVMOpts        = "NEXUS_BENCH_JVM_OPTS"
	envImage          = "NEXUS_BENCH_IMAGE"
	envImageTag       = "NEXUS_BENCH_IMAGE_TAG"
	envUpstreamURL    = "NEXUS_BENCH_UPSTREAM_URL"
	envAnonymous      = "NEXUS_BENCH_ANONYMOUS"
	envAcceptEULA     = "NEXUS_BENCH_ACCEPT_EULA"
	envHosts          = "NEXUS_BENCH_HOSTS"
	envTimeout        = "NEXUS_BENCH_TIMEOUT"
	envDeleteTimeout  = "NEXUS_BENCH_DELETE_TIMEOUT"
	envSecrets        = "NEXUS_BENCH_SECRETS"
	envDescriptor     = "NEXUS_BENCH_DESCRIPTOR"
)

const (
	// Defaults of D-B and D-C. The NodePorts were free on 2026-11-16; the block
	// 30483-30489 stays reserved for the next NX stories.
	defaultContext        = "datomir"
	defaultNamespace      = "nexus-bench"
	defaultHTTPNodePort   = 30481
	defaultDockerNodePort = 30482
	defaultImage          = "docker.io/sonatype/nexus3"
	defaultImageTag       = "3.98.1"
	defaultJVMOpts        = "-Xms1g -Xmx1g -XX:MaxDirectMemorySize=1g -Djava.util.prefs.userRoot=/nexus-data/javaprefs"

	nodePortMin = 30000
	nodePortMax = 32767

	// zotRegistry is the registry the Kyverno policy verify-tvp-images guards:
	// an image from it must be signed, and the bench image is not.
	zotRegistry = "tvp-zot.incus:5000"

	maxJVMOptsLen   = 512
	maxImageLen     = 255
	maxHostLen      = 253
	maxPathLen      = 4096
	maxContextLen   = 253 // a kubeconfig context name is bounded like a DNS name
	maxNamespaceLen = 63  // a DNS-1123 label
	maxTCPPort      = 65535
)

var (
	// dns1123Label is a Kubernetes namespace name; the length is checked apart.
	dns1123Label = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	// contextName is kept to characters that are safe in a file name and in a
	// kubectl argument: the context names a directory of the secrets path.
	contextName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]*$`)
	// cpuQuantity is a Kubernetes CPU quantity: whole or decimal cores, or milli-cores.
	cpuQuantity = regexp.MustCompile(`^([0-9]+(\.[0-9]+)?|[0-9]+m)$`)
	// byteQuantity is a Kubernetes quantity of bytes: a number and an optional
	// binary (Ki..Ei) or decimal (k, M, G, T, P, E) suffix. Exponent forms are not accepted.
	byteQuantity = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?$`)
	// jvmOptsChars rules out quotes, backslashes, braces, '#', '&', '*', '!',
	// '|', '>' and every control character (newlines included), so a value
	// cannot end a YAML scalar or start another node.
	jvmOptsChars = regexp.MustCompile(`^[A-Za-z0-9 _.,:=/+@%-]+$`)
	// jvmOptsStart keeps the first character from being a YAML indicator: a
	// letter, a digit, or '-' followed by a non-space (so not "- x", a sequence entry).
	jvmOptsStart = regexp.MustCompile(`^([A-Za-z0-9]|-[^ ])`)
	imageChars   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:-]*$`)
	imageTag     = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	hostName     = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)
	urlChars     = regexp.MustCompile(`^[A-Za-z0-9._~:/%-]+$`)
)

// reservedNamespaces are namespaces the bench must never create, adopt or
// delete, whatever their label says. Matching is on the lower-cased name.
var reservedNamespaces = map[string]bool{
	"default":            true,
	"flux-system":        true,
	"local-path-storage": true,
	"traefik":            true,
	"kyverno":            true,
	"observability":      true,
	"plane":              true,
}

// reservedNamespacePrefixes are the families of namespaces owned by someone else.
var reservedNamespacePrefixes = []string{"kube-", "app-"}

// Params is everything that varies between two runs of the bench. Resolve it
// with Defaults, then FromEnv, then the command flags, then Validate before any
// call to the cluster: the manifests are rendered from validated values only.
type Params struct {
	Context, Namespace, Image, ImageTag string
	HTTPNodePort, DockerNodePort        int
	CPURequest, CPULimit                string
	MemoryRequest, MemoryLimit          string
	StorageSize, JVMOpts                string
	UpstreamURL                         string
	Anonymous, AcceptEULA               bool
	// Hosts are the addresses the NodePorts are tried on; nil means the tool
	// works them out (127.0.0.1 and the node's InternalIP).
	Hosts                       []string
	Timeout, DeleteTimeout      time.Duration
	SecretsPath, DescriptorPath string
}

// Defaults returns the parameters of the bench as D-B and D-C define them.
// Hosts, SecretsPath and DescriptorPath stay empty: the tool derives them.
func Defaults() Params {
	return Params{
		Context:        defaultContext,
		Namespace:      defaultNamespace,
		Image:          defaultImage,
		ImageTag:       defaultImageTag,
		HTTPNodePort:   defaultHTTPNodePort,
		DockerNodePort: defaultDockerNodePort,
		CPURequest:     "500m",
		CPULimit:       "2",
		MemoryRequest:  "3Gi",
		MemoryLimit:    "4Gi",
		StorageSize:    "5Gi",
		JVMOpts:        defaultJVMOpts,
		UpstreamURL:    upstreamFor(defaultNamespace),
		Anonymous:      true,
		Timeout:        10 * time.Minute,
		DeleteTimeout:  3 * time.Minute,
	}
}

// upstreamFor is the in-cluster URL of the upstream registry NX-02 creates in a namespace.
func upstreamFor(namespace string) string {
	return "http://bench-upstream." + namespace + ".svc.cluster.local:5000"
}

// ImageRef returns the image reference of the Nexus container, "image:tag".
func (p Params) ImageRef() string {
	return p.Image + ":" + p.ImageTag
}

// envSetter applies the value of one non-empty variable to p.
type envSetter struct {
	name  string
	apply func(p *Params, value string) error
}

func stringSetter(name string, field func(p *Params) *string) envSetter {
	return envSetter{name, func(p *Params, v string) error {
		*field(p) = v
		return nil
	}}
}

func portSetter(name string, field func(p *Params) *int) envSetter {
	return envSetter{name, func(p *Params, v string) error {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s: %q is not a whole number", name, v)
		}
		*field(p) = n
		return nil
	}}
}

func durationSetter(name string, field func(p *Params) *time.Duration) envSetter {
	return envSetter{name, func(p *Params, v string) error {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("%s: %q is not a duration such as 90s or 10m", name, v)
		}
		*field(p) = d
		return nil
	}}
}

func boolSetter(name string, field func(p *Params) *bool) envSetter {
	return envSetter{name, func(p *Params, v string) error {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%s: %q is not true or false", name, v)
		}
		*field(p) = b
		return nil
	}}
}

// envSetters lists every variable but the namespace, which FromEnv handles
// itself because it also moves the upstream URL.
var envSetters = []envSetter{
	stringSetter(envContext, func(p *Params) *string { return &p.Context }),
	portSetter(envHTTPNodePort, func(p *Params) *int { return &p.HTTPNodePort }),
	portSetter(envDockerNodePort, func(p *Params) *int { return &p.DockerNodePort }),
	stringSetter(envCPURequest, func(p *Params) *string { return &p.CPURequest }),
	stringSetter(envCPULimit, func(p *Params) *string { return &p.CPULimit }),
	stringSetter(envMemoryRequest, func(p *Params) *string { return &p.MemoryRequest }),
	stringSetter(envMemoryLimit, func(p *Params) *string { return &p.MemoryLimit }),
	stringSetter(envStorageSize, func(p *Params) *string { return &p.StorageSize }),
	stringSetter(envJVMOpts, func(p *Params) *string { return &p.JVMOpts }),
	stringSetter(envImage, func(p *Params) *string { return &p.Image }),
	stringSetter(envImageTag, func(p *Params) *string { return &p.ImageTag }),
	stringSetter(envUpstreamURL, func(p *Params) *string { return &p.UpstreamURL }),
	boolSetter(envAnonymous, func(p *Params) *bool { return &p.Anonymous }),
	// Accepting a license is the user's decision: only the exact word "yes" counts.
	{envAcceptEULA, func(p *Params, v string) error {
		p.AcceptEULA = v == "yes"
		return nil
	}},
	{envHosts, func(p *Params, v string) error {
		p.Hosts = splitList(v)
		return nil
	}},
	durationSetter(envTimeout, func(p *Params) *time.Duration { return &p.Timeout }),
	durationSetter(envDeleteTimeout, func(p *Params) *time.Duration { return &p.DeleteTimeout }),
	stringSetter(envSecrets, func(p *Params) *string { return &p.SecretsPath }),
	stringSetter(envDescriptor, func(p *Params) *string { return &p.DescriptorPath }),
}

// FromEnv returns base with every non-empty NEXUS_BENCH_* variable applied; base
// itself is left untouched. A value that cannot be parsed is an error that names
// its variable. Setting only NEXUS_BENCH_NAMESPACE moves the upstream URL to the
// new namespace, unless base already carries a custom one or
// NEXUS_BENCH_UPSTREAM_URL is set. FromEnv checks the shape of numbers,
// durations and booleans only; Validate checks the values.
func FromEnv(base Params, getenv func(string) string) (Params, error) {
	p := base
	if base.Hosts != nil {
		p.Hosts = append([]string(nil), base.Hosts...)
	}

	followsNamespace := base.UpstreamURL == upstreamFor(base.Namespace)
	if v := getenv(envNamespace); v != "" {
		p.Namespace = v
	}
	for _, s := range envSetters {
		v := getenv(s.name)
		if v == "" {
			continue
		}
		if err := s.apply(&p, v); err != nil {
			return base, err
		}
	}
	if followsNamespace && getenv(envUpstreamURL) == "" {
		p.UpstreamURL = upstreamFor(p.Namespace)
	}
	return p, nil
}

// splitList splits a comma-separated list, trimming blanks and dropping empty items.
func splitList(v string) []string {
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// Validate refuses any value the bench must not act on or render into a
// manifest. It reports every problem at once, each naming its variable, and
// never contacts the cluster.
func (p Params) Validate() error {
	var errs []error
	add := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	add(validateContext(p.Context))
	add(validateNamespace(p.Namespace))
	add(validateNodePort(envHTTPNodePort, p.HTTPNodePort))
	add(validateNodePort(envDockerNodePort, p.DockerNodePort))
	if p.HTTPNodePort == p.DockerNodePort {
		add(fmt.Errorf("%s and %s are both %d: the two NodePorts must differ", envHTTPNodePort, envDockerNodePort, p.HTTPNodePort))
	}
	add(validateQuantity(envCPURequest, p.CPURequest, parseCPU, "cores such as 2 or 500m"))
	add(validateQuantity(envCPULimit, p.CPULimit, parseCPU, "cores such as 2 or 500m"))
	add(validateQuantity(envMemoryRequest, p.MemoryRequest, parseBytes, "bytes such as 3Gi"))
	add(validateQuantity(envMemoryLimit, p.MemoryLimit, parseBytes, "bytes such as 4Gi"))
	add(validateQuantity(envStorageSize, p.StorageSize, parseBytes, "bytes such as 5Gi"))
	add(validateRequestLimit(envCPURequest, p.CPURequest, envCPULimit, p.CPULimit, parseCPU))
	add(validateRequestLimit(envMemoryRequest, p.MemoryRequest, envMemoryLimit, p.MemoryLimit, parseBytes))
	add(validateJVMOpts(p.JVMOpts))
	add(validateImage(p.Image, p.ImageTag))
	add(validateUpstreamURL(p.UpstreamURL))
	add(validateHosts(p.Hosts))
	add(validateTimeout(envTimeout, p.Timeout))
	add(validateTimeout(envDeleteTimeout, p.DeleteTimeout))
	add(validatePath(envSecrets, p.SecretsPath))
	add(validatePath(envDescriptor, p.DescriptorPath))
	return errors.Join(errs...)
}

func validateContext(v string) error {
	if len(v) > maxContextLen || !contextName.MatchString(v) {
		return fmt.Errorf("%s: %q is not a usable context name (letters, digits, '.', '_', '@' and '-', starting with a letter or digit)", envContext, v)
	}
	return nil
}

func validateNamespace(v string) error {
	lower := strings.ToLower(v)
	reserved := reservedNamespaces[lower]
	for _, prefix := range reservedNamespacePrefixes {
		reserved = reserved || strings.HasPrefix(lower, prefix)
	}
	if reserved {
		return fmt.Errorf("%s: namespace %q belongs to the cluster or to another project and is never used by the bench (refused: default, kube-*, app-*, flux-system, local-path-storage, traefik, kyverno, observability, plane)", envNamespace, v)
	}
	if len(v) > maxNamespaceLen || !dns1123Label.MatchString(v) {
		return fmt.Errorf("%s: %q is not a Kubernetes namespace name (lower-case letters, digits and '-', %d characters at most, starting and ending with a letter or digit)", envNamespace, v, maxNamespaceLen)
	}
	return nil
}

func validateNodePort(name string, port int) error {
	if port < nodePortMin || port > nodePortMax {
		return fmt.Errorf("%s: %d is outside the NodePort range %d-%d", name, port, nodePortMin, nodePortMax)
	}
	return nil
}

// quantityParser turns a Kubernetes quantity into a comparable amount (milli-cores
// or bytes); ok is false when the text is not a quantity of that kind.
type quantityParser func(v string) (amount *big.Rat, ok bool)

// parseCPU returns a CPU quantity in milli-cores. big.Rat keeps "0.0005" and
// huge values exact, which an int64 would not.
func parseCPU(v string) (*big.Rat, bool) {
	if !cpuQuantity.MatchString(v) {
		return nil, false
	}
	number, milli := strings.CutSuffix(v, "m")
	r, ok := new(big.Rat).SetString(number)
	if !ok {
		return nil, false
	}
	if !milli {
		r.Mul(r, big.NewRat(1000, 1))
	}
	return r, true
}

// byteMultipliers maps the suffixes byteQuantity accepts to their value in bytes.
var byteMultipliers = map[string]*big.Int{
	"":   big.NewInt(1),
	"Ki": new(big.Int).Lsh(big.NewInt(1), 10),
	"Mi": new(big.Int).Lsh(big.NewInt(1), 20),
	"Gi": new(big.Int).Lsh(big.NewInt(1), 30),
	"Ti": new(big.Int).Lsh(big.NewInt(1), 40),
	"Pi": new(big.Int).Lsh(big.NewInt(1), 50),
	"Ei": new(big.Int).Lsh(big.NewInt(1), 60),
	"k":  big.NewInt(1e3),
	"M":  big.NewInt(1e6),
	"G":  big.NewInt(1e9),
	"T":  big.NewInt(1e12),
	"P":  big.NewInt(1e15),
	"E":  big.NewInt(1e18),
}

// parseBytes returns a quantity of bytes.
func parseBytes(v string) (*big.Rat, bool) {
	m := byteQuantity.FindStringSubmatch(v)
	if m == nil {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(m[1])
	if !ok {
		return nil, false
	}
	return r.Mul(r, new(big.Rat).SetInt(byteMultipliers[m[2]])), true
}

func validateQuantity(name, v string, parse quantityParser, hint string) error {
	amount, ok := parse(v)
	if !ok {
		return fmt.Errorf("%s: %q is not a Kubernetes quantity of %s", name, v, hint)
	}
	if amount.Sign() == 0 {
		return fmt.Errorf("%s: %q is zero; the quantity must be greater than zero", name, v)
	}
	return nil
}

// validateRequestLimit refuses a request above its limit, which Kubernetes
// rejects at admission. A value that is not a quantity is reported by
// validateQuantity, not here.
func validateRequestLimit(requestName, request, limitName, limit string, parse quantityParser) error {
	req, okReq := parse(request)
	lim, okLim := parse(limit)
	if okReq && okLim && req.Cmp(lim) > 0 {
		return fmt.Errorf("%s and %s: the request %q is above the limit %q; the request must not exceed the limit", requestName, limitName, request, limit)
	}
	return nil
}

func validateJVMOpts(v string) error {
	if v == "" || len(v) > maxJVMOptsLen || !jvmOptsChars.MatchString(v) {
		return fmt.Errorf("%s: %q must be 1-%d characters among letters, digits, space and _ . , : = / + @ %% -", envJVMOpts, v, maxJVMOptsLen)
	}
	// A leading indicator ("- ", "@", "%", ",", ":", a space) would stop the value
	// from being a plain YAML scalar when it is written unquoted.
	if !jvmOptsStart.MatchString(v) {
		return fmt.Errorf("%s: %q must start with a letter, a digit, or '-' followed by a non-space character", envJVMOpts, v)
	}
	// ": " and a trailing ":" would turn a plain YAML scalar into a mapping key.
	if strings.Contains(v, ": ") || strings.HasSuffix(v, ":") {
		return fmt.Errorf("%s: %q must not hold \": \" or end with a colon", envJVMOpts, v)
	}
	return nil
}

func validateImage(image, tag string) error {
	if strings.HasPrefix(strings.ToLower(image), zotRegistry) {
		return fmt.Errorf("%s: %q is in %s, where the Kyverno policy verify-tvp-images only admits signed images and the bench image is not signed; use an image from another registry", envImage, image, zotRegistry)
	}
	if len(image) > maxImageLen || !imageChars.MatchString(image) {
		return fmt.Errorf("%s: %q is not an image name without tag or digest", envImage, image)
	}
	if !imageTag.MatchString(tag) {
		return fmt.Errorf("%s: %q is not an image tag", envImageTag, tag)
	}
	return nil
}

func validateUpstreamURL(v string) error {
	u, err := url.Parse(v)
	if err != nil || !urlChars.MatchString(v) {
		return fmt.Errorf("%s: %q is not a plain http or https URL", envUpstreamURL, v)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("%s: %q must be http:// or https:// followed by a host, without credentials", envUpstreamURL, v)
	}
	// url.Parse accepts an empty port ("http://a:") and any digits ("a:99999").
	if port := u.Port(); port != "" || strings.HasSuffix(u.Host, ":") {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > maxTCPPort {
			return fmt.Errorf("%s: %q has a port outside 1-%d", envUpstreamURL, v, maxTCPPort)
		}
	}
	return nil
}

func validateHosts(hosts []string) error {
	for _, h := range hosts {
		if net.ParseIP(h) == nil && (len(h) > maxHostLen || !hostName.MatchString(h)) {
			return fmt.Errorf("%s: %q is not an IP address or a host name", envHosts, h)
		}
	}
	return nil
}

func validateTimeout(name string, d time.Duration) error {
	if d <= 0 {
		return fmt.Errorf("%s: %s must be a positive duration", name, d)
	}
	return nil
}

// validatePath refuses control characters; an empty path means "derived".
func validatePath(name, v string) error {
	if len(v) > maxPathLen || strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("%s: the path is too long or holds a control character", name)
	}
	return nil
}
