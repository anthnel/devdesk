// Package contextfiles holds the context files of the "default registry" epic's
// non-regression tests (DEVDESK-665): the shapes an existing user's context
// directory can hold, written by hand or by an older build. It is pure data, so
// the config, app and main tests share one list. Import it from _test.go files
// only.
package contextfiles

// Shape is one context file and what loading it must yield.
type Shape struct {
	// Name is a valid context name; the file is config-<Name>.yaml.
	Name string
	// Content is the file, byte for byte.
	Content string
	// URLs are the registry URLs a load must return, in order, once each.
	URLs []string
	// LegacySecret marks a file MigrateLegacySecrets rewrites (it carries a
	// registry.password): its bytes are expected to change, its registries not.
	LegacySecret bool
}

// Shapes returns a fresh copy of the list.
func Shapes() []Shape {
	return []Shape{
		{
			Name:    "no-section",
			Content: "app:\n    theme: dark\n",
		},
		{
			Name:    "empty-file",
			Content: "",
		},
		{
			Name:    "empty-list",
			Content: "registry:\n    registries: []\n",
		},
		{
			Name:    "empty-list-empty-url",
			Content: "registry:\n    url: \"\"\n    registries: []\n",
		},
		{
			Name:    "empty-url-only",
			Content: "registry:\n    url: \"\"\n",
		},
		{
			Name: "several",
			Content: "registry:\n    registries:\n" +
				"        - slug: corp\n          url: registry.corp.example\n          auth_mode: credentials\n          username: bob\n" +
				"        - slug: ghcr-io\n          url: ghcr.io\n          auth_mode: anonymous\n" +
				"        - slug: quay-io\n          url: quay.io\n          auth_mode: anonymous\n",
			URLs: []string{"registry.corp.example", "ghcr.io", "quay.io"},
		},
		{
			Name:    "legacy-url",
			Content: "registry:\n    url: registry.corp.example\n    username: bob\n",
			URLs:    []string{"registry.corp.example"},
		},
		{
			Name:         "legacy-url-and-secret",
			Content:      "registry:\n    url: registry.corp.example\n    username: bob\n    password: s3cret\n",
			URLs:         []string{"registry.corp.example"},
			LegacySecret: true,
		},
		{
			Name:    "legacy-url-docker-io",
			Content: "registry:\n    url: docker.io\n",
			URLs:    []string{"docker.io"},
		},
		{
			Name: "explicit-docker-io",
			Content: "registry:\n    registries:\n" +
				"        - slug: docker-io\n          url: docker.io\n          auth_mode: anonymous\n",
			URLs: []string{"docker.io"},
		},
		{
			Name: "docker-io-without-slug",
			Content: "registry:\n    registries:\n" +
				"        - url: docker.io\n          auth_mode: anonymous\n",
			URLs: []string{"docker.io"},
		},
		{
			// The list wins: the legacy key is migrated only into an empty list.
			Name: "legacy-url-and-docker-io-entry",
			Content: "registry:\n    url: docker.io\n    registries:\n" +
				"        - slug: docker-io\n          url: docker.io\n          auth_mode: anonymous\n",
			URLs: []string{"docker.io"},
		},
		{
			Name: "legacy-url-and-other-entry",
			Content: "registry:\n    url: registry.corp.example\n    registries:\n" +
				"        - slug: ghcr-io\n          url: ghcr.io\n          auth_mode: anonymous\n",
			URLs: []string{"ghcr.io"},
		},
	}
}
