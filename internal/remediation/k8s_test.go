package remediation

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/anthnel/devdesk/internal/scan"
)

// webDeployment is the manifest the real Trivy run was recorded on (0.71):
// KSV-0017 and KSV-0001 on 'api' span lines 13-16, KSV-0001 on 'sidecar'
// spans 17-18. The findings below carry exactly those spans and messages.
const webDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels: {app: web}
  template:
    metadata:
      labels: {app: web}
    spec:
      containers:
        - name: api
          image: api:1.0
          securityContext:
            privileged: true
        - name: sidecar
          image: side:1.0
`

func ksv(id, container string, line, endLine int) scan.Finding {
	field := map[string]string{"KSV-0001": "allowPrivilegeEscalation", "KSV-0017": "privileged"}[id]
	return scan.Finding{
		ID: id, Source: scan.SourceTrivyMisconfig, File: "deploy.yaml", IaCType: "kubernetes",
		Line: line, EndLine: endLine,
		Message: "Container '" + container + "' of Deployment 'web' should set 'securityContext." + field + "' to false",
	}
}

// containerSetting reads a container's securityContext key back from the
// rewritten file, so the tests judge the YAML a cluster would read and not the
// text they expect.
func containerSetting(t *testing.T, src, container, key string) string {
	t.Helper()
	var doc struct {
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Name            string         `yaml:"name"`
						SecurityContext map[string]any `yaml:"securityContext"`
					} `yaml:"containers"`
				} `yaml:"spec"`
			} `yaml:"template"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("the fix produced YAML that does not parse: %v\n%s", err, src)
	}
	for _, c := range doc.Spec.Template.Spec.Containers {
		if c.Name == container {
			if v, ok := c.SecurityContext[key]; ok {
				return strings.ToLower(strings.TrimSpace(toString(v)))
			}
			return ""
		}
	}
	t.Fatalf("container %q vanished", container)
	return ""
}

func toString(v any) string {
	if b, ok := v.(bool); ok {
		if b {
			return "true"
		}
		return "false"
	}
	s, _ := v.(string)
	return s
}

func TestPrivilegedBecomesFalse(t *testing.T) {
	out, reason := apply(t, webDeployment, ksv("KSV-0017", "api", 13, 16))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if got := containerSetting(t, out, "api", "privileged"); got != "false" {
		t.Errorf("privileged = %q, want false", got)
	}
	if strings.Count(out, "\n") != strings.Count(webDeployment, "\n") {
		t.Error("a token swap changed the number of lines")
	}
}

// The API server refuses allowPrivilegeEscalation: false on a privileged
// container. The rule would pass and the manifest could no longer be applied.
func TestEscalationIsNotSetOnAPrivilegedContainer(t *testing.T) {
	if _, reason := apply(t, webDeployment, ksv("KSV-0001", "api", 13, 16)); reason != ReasonPrivilegedContainer {
		t.Errorf("reason = %q, want %q", reason, ReasonPrivilegedContainer)
	}
}

// A container with no securityContext gets one, above its first key that
// starts a line, at that key's indentation — the name on the dash line is
// not one.
func TestEscalationIsAddedWithTheSecurityContext(t *testing.T) {
	out, reason := apply(t, webDeployment, ksv("KSV-0001", "sidecar", 17, 18))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if got := containerSetting(t, out, "sidecar", "allowPrivilegeEscalation"); got != "false" {
		t.Errorf("allowPrivilegeEscalation = %q, want false\n%s", got, out)
	}
	want := "        - name: sidecar\n          securityContext:\n            allowPrivilegeEscalation: false\n          image: side:1.0\n"
	if !strings.HasSuffix(out, want) {
		t.Errorf("sidecar reads:\n%s\nwant it to end with:\n%s", out, want)
	}
	// The other container is untouched.
	if got := containerSetting(t, out, "api", "privileged"); got != "true" {
		t.Errorf("api's privileged changed to %q", got)
	}
}

func TestEscalationJoinsAnExistingSecurityContext(t *testing.T) {
	src := strings.Replace(webDeployment, "privileged: true", "runAsUser: 1000 # keep this comment", 1)

	out, reason := apply(t, src, ksv("KSV-0001", "api", 13, 16))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if got := containerSetting(t, out, "api", "allowPrivilegeEscalation"); got != "false" {
		t.Errorf("allowPrivilegeEscalation = %q, want false", got)
	}
	if !strings.Contains(out, "            allowPrivilegeEscalation: false\n            runAsUser: 1000 # keep this comment\n") {
		t.Errorf("the key was not inserted at the context's indentation, or the comment was lost:\n%s", out)
	}
}

func TestEscalationTrueIsSwapped(t *testing.T) {
	src := strings.Replace(webDeployment, "privileged: true", "allowPrivilegeEscalation: true", 1)

	out, reason := apply(t, src, ksv("KSV-0001", "api", 13, 16))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if got := containerSetting(t, out, "api", "allowPrivilegeEscalation"); got != "false" {
		t.Errorf("allowPrivilegeEscalation = %q, want false", got)
	}
}

// A CRLF file gets CRLF lines: an inserted "\n" would leave the file with two
// line endings.
func TestAnInsertionKeepsTheFilesLineEndings(t *testing.T) {
	src := strings.ReplaceAll(webDeployment, "\n", "\r\n")

	out, reason := apply(t, src, ksv("KSV-0001", "sidecar", 17, 18))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if strings.Count(out, "\n") != strings.Count(out, "\r\n") {
		t.Errorf("a bare \\n was inserted into a CRLF file:\n%q", out)
	}
}

func TestTheKubernetesFixesDecline(t *testing.T) {
	inline := strings.Replace(webDeployment, "securityContext:\n            privileged: true", "securityContext: {}", 1)
	sysAdmin := strings.Replace(webDeployment, "privileged: true", "capabilities:\n              add: [SYS_ADMIN]", 1)
	helm := ksv("KSV-0017", "api", 13, 16)
	helm.IaCType = "helm"

	tests := []struct {
		name string
		src  string
		f    scan.Finding
		want string
	}{
		{"a Helm template", webDeployment, helm, ReasonNotAManifest},
		{"a container the file no longer has", webDeployment, ksv("KSV-0017", "gone", 13, 16), ReasonContainerNotFound},
		{"a container outside the reported lines", webDeployment, ksv("KSV-0017", "api", 17, 18), ReasonContainerNotFound},
		{"an inline securityContext", inline, ksv("KSV-0001", "api", 13, 15), ReasonInlineMapping},
		{"CAP_SYS_ADMIN", sysAdmin, ksv("KSV-0001", "api", 13, 17), ReasonSysAdmin},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, reason := apply(t, tt.src, tt.f); reason != tt.want {
				t.Errorf("reason = %q, want %q", reason, tt.want)
			}
		})
	}
}

func removedAPI(line int) scan.Finding {
	return scan.Finding{ID: scan.K8sAPIRemovedID, Source: scan.SourceKubeconform, File: "jobs.yaml", IaCType: "kubernetes", Line: line, EndLine: line}
}

const removedAPIs = `apiVersion: batch/v1beta1 # nightly job
kind: CronJob
metadata:
  name: nightly
---
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: web
`

// Only where the deprecation guide says "No notable changes" is renaming the
// apiVersion the whole migration.
func TestARemovedAPIIsRenamedOnlyWhenThatIsTheWholeMigration(t *testing.T) {
	out, reason := apply(t, removedAPIs, removedAPI(1))
	if reason != "" {
		t.Fatalf("CronJob declined: %s", reason)
	}
	if !strings.HasPrefix(out, "apiVersion: batch/v1 # nightly job\n") {
		t.Errorf("CronJob not moved to batch/v1, or its comment lost:\n%s", out)
	}
	if strings.Contains(out[strings.Index(out, "---"):], "networking.k8s.io/v1\n") {
		t.Error("the Ingress was touched too")
	}

	if _, reason := apply(t, removedAPIs, removedAPI(6)); reason != ReasonNoSimpleMigration {
		t.Errorf("Ingress: reason = %q, want %q", reason, ReasonNoSimpleMigration)
	}
}

// A schema finding from a rendered chart points at a template: nothing there
// is the YAML that was validated.
func TestARemovedAPIInARenderedChartIsNotRewritten(t *testing.T) {
	f := removedAPI(1)
	f.IaCType = "helm"
	if _, reason := apply(t, removedAPIs, f); reason != ReasonNotAManifest {
		t.Errorf("reason = %q, want %q", reason, ReasonNotAManifest)
	}
}
