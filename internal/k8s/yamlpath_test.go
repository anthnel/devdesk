package k8s

import "testing"

const multiDoc = `# comment
apiVersion: v1
kind: Service
metadata:
  name: api
spec:
  ports:
    - port: 80
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  template:
    spec:
      containers:
        - name: api
          image: api:1
          imagePullPolicyy: Always
          securityContext:
            privileged: true
        - name: sidecar
          image: side:1
          command:
            - |
              echo one
              echo two
`

func TestLocateFollowsAPointerIntoTheRightDocument(t *testing.T) {
	tests := []struct {
		name, kind, resource, pointer string
		want                          Span
		ok                            bool
	}{
		{"the document itself", "Deployment", "api", "", Span{10, 28}, true},
		{"a top-level key", "Deployment", "api", "/apiVersion", Span{10, 10}, true},
		{"a key in a sequence item", "Deployment", "api", "/spec/template/spec/containers/0/imagePullPolicyy", Span{20, 20}, true},
		{"a nested mapping spans its value", "Deployment", "api", "/spec/template/spec/containers/0/securityContext", Span{21, 22}, true},
		{"a sequence item", "Deployment", "api", "/spec/template/spec/containers/1", Span{23, 28}, true},
		{"a literal block ends where its text does", "Deployment", "api", "/spec/template/spec/containers/1/command", Span{25, 28}, true},
		{"the other document of the same name", "Service", "api", "/spec/ports/0/port", Span{8, 8}, true},
		{"an empty name takes the first of the kind", "Service", "", "/kind", Span{3, 3}, true},
		{"a missing key is not found, not the nearest line", "Deployment", "api", "/spec/replicas", Span{}, false},
		{"an index out of range", "Deployment", "api", "/spec/template/spec/containers/5", Span{}, false},
		{"no such resource", "Deployment", "web", "", Span{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Locate([]byte(multiDoc), tt.kind, tt.resource, tt.pointer)
			if ok != tt.ok || got != tt.want {
				t.Errorf("Locate(%q) = %+v, %v; want %+v, %v", tt.pointer, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// RFC 6901 escapes "/" and "~" inside a key; an annotation key is the usual case.
func TestLocateUnescapesThePointer(t *testing.T) {
	content := []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: p\n  annotations:\n    example.com/a~b: x\n")

	got, ok := Locate(content, "Pod", "p", "/metadata/annotations/example.com~1a~0b")

	if !ok || got.Line != 6 {
		t.Errorf("Locate = %+v, %v; want line 6", got, ok)
	}
}
