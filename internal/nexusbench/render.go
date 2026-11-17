package nexusbench

import (
	"bytes"
	"embed"
	"fmt"
	"strconv"
	"text/template"
)

//go:embed manifests/*.yaml.tmpl
var manifestFS embed.FS

// Manifest files in the order Render writes them: the Namespace comes first so
// that an apply creates it before the objects it holds.
const (
	namespaceTemplate = "00-namespace.yaml.tmpl"
)

var manifestTemplates = []string{
	namespaceTemplate,
	"10-pvc.yaml.tmpl",
	"20-deployment.yaml.tmpl",
	"30-service.yaml.tmpl",
	"40-upstream-pvc.yaml.tmpl",
	"50-upstream-deployment.yaml.tmpl",
	"60-upstream-service.yaml.tmpl",
}

// templates parses the embedded manifests. quote is the only function they can
// call, and it is how every string value reaches the output.
var templates = template.Must(template.New("manifests").
	Funcs(template.FuncMap{"q": quote}).
	Option("missingkey=error").
	ParseFS(manifestFS, "manifests/*.yaml.tmpl"))

// quote writes a string as a YAML double-quoted scalar. A Go string literal is
// valid YAML double-quoted syntax (the escapes \a \b \t \n \v \f \r \" \\ \xNN
// \uNNNN \UNNNNNNNN are common to both), so the value is always read back as the
// same string, never as a number, a boolean, a null or a mapping.
func quote(s string) string { return strconv.Quote(s) }

// manifestData is what the templates see: the validated values the manifests
// carry, and nothing else.
type manifestData struct {
	Namespace                                        string
	Image, UpstreamImage                             string
	CPURequest, CPULimit, MemoryRequest, MemoryLimit string
	StorageSize, JVMOpts                             string
	HTTPNodePort, DockerNodePort                     int
}

func newManifestData(p Params) manifestData {
	return manifestData{
		Namespace:      p.Namespace,
		Image:          p.ImageRef(),
		UpstreamImage:  p.UpstreamImageRef(),
		CPURequest:     p.CPURequest,
		CPULimit:       p.CPULimit,
		MemoryRequest:  p.MemoryRequest,
		MemoryLimit:    p.MemoryLimit,
		StorageSize:    p.StorageSize,
		JVMOpts:        p.JVMOpts,
		HTTPNodePort:   p.HTTPNodePort,
		DockerNodePort: p.DockerNodePort,
	}
}

// Render returns the manifests of the bench as a multi-document YAML stream: the
// Namespace, the PersistentVolumeClaim, the Deployment and the Service of Nexus,
// then the PersistentVolumeClaim, the Deployment and the Service of the upstream
// registry, in that order. The parameters are validated first, and the same
// parameters always give the same bytes. Parameters the manifests do not carry
// (context, upstream URL, EULA, hosts, timeouts, paths) do not change the output.
func Render(p Params) ([]byte, error) {
	return render(p, manifestTemplates)
}

// RenderNamespace returns the Namespace document alone, the first one of Render.
func RenderNamespace(p Params) ([]byte, error) {
	return render(p, []string{namespaceTemplate})
}

func render(p Params, names []string) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	data := newManifestData(p)
	var out bytes.Buffer
	for i, name := range names {
		if i > 0 {
			out.WriteString("---\n")
		}
		if err := templates.ExecuteTemplate(&out, name, data); err != nil {
			return nil, fmt.Errorf("render %s: %w", name, err)
		}
	}
	return out.Bytes(), nil
}
