package fileicon

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// TestTheBasenameIsConsultedBeforeTheExtension is the rule this package
// borrows from internal/viewer/detect.go, and the one that cannot be got
// wrong quietly: `Dockerfile` has no extension at all, and `Dockerfile.dev`
// has `.dev`, which is in no table — an extension-first lookup would settle
// both as plain files before the name ever got a say.
func TestTheBasenameIsConsultedBeforeTheExtension(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"Dockerfile", theme.IconDocker},
		{"Dockerfile.dev", theme.IconDocker},
		{"docker-compose.yml", theme.IconDocker},
		{"go.mod", theme.IconGo},
		{"Cargo.toml", theme.IconRust},
		{"package.json", theme.IconNode},
		{"Makefile", theme.IconTools},
		{"mise.toml", theme.IconTools},
	}
	for _, c := range cases {
		if got := For(c.name); got != c.want {
			t.Errorf("For(%q) = %q, want the basename's glyph %q", c.name, got, c.want)
		}
	}
}

// TestABasenameBeatsItsOwnExtension is the same rule stated as a conflict.
// Every name here has an extension that resolves to something else, so a table
// consulted in the wrong order passes the test above and fails this one.
func TestABasenameBeatsItsOwnExtension(t *testing.T) {
	cases := []struct{ name, loses string }{
		{"docker-compose.yml", For("values.yml")},
		{"Cargo.toml", For("config.toml")},
		{"package.json", For("settings.json")},
		{"mise.toml", For("config.toml")},
		{".gitlab-ci.yml", For("values.yml")},
	}
	for _, c := range cases {
		if got := For(c.name); got == c.loses {
			t.Errorf("For(%q) = %q — the extension won over the basename", c.name, got)
		}
	}
}

// TestGitlabCiIsAFilenameAndNotVocabulary states the distinction the
// vocabtest guard makes the reader confront. The file is called
// `.gitlab-ci.yml` on disk whatever forge a context targets, so the glyph is a
// fact about the file rather than about the platform.
func TestGitlabCiIsAFilenameAndNotVocabulary(t *testing.T) {
	if got := For(".gitlab-ci.yml"); got != theme.IconGitlab {
		t.Errorf("For(.gitlab-ci.yml) = %q, want the GitLab glyph", got)
	}
	if For(".gitlab-ci.yaml") != For(".gitlab-ci.yml") {
		t.Error("the two spellings of the pipeline file disagree")
	}
}

// TestTheLookupIsCaseInsensitive covers README, Makefile and LICENSE, which
// are written in every case there is.
func TestTheLookupIsCaseInsensitive(t *testing.T) {
	for _, name := range []string{"MAKEFILE", "Makefile", "makefile"} {
		if got := For(name); got != theme.IconTools {
			t.Errorf("For(%q) = %q, want the same glyph as makefile", name, got)
		}
	}
}

// TestAReadmeIsAReadmeWhateverItsExtension is why readmePrefix exists: the
// extension would win and give Markdown, which is right for README.md and
// wrong for README.rst.
func TestAReadmeIsAReadmeWhateverItsExtension(t *testing.T) {
	bare := For("README")
	for _, name := range []string{"README.md", "README.rst", "readme.txt"} {
		if got := For(name); got != bare {
			t.Errorf("For(%q) = %q, want the same glyph as a bare README (%q)", name, got, bare)
		}
	}
}

// TestAFullPathResolvesOnItsBasename keeps the lookup from depending on where
// the caller got the name — ws hands it an entry name, but a path must work.
func TestAFullPathResolvesOnItsBasename(t *testing.T) {
	if For("/home/me/projects/api/go.mod") != For("go.mod") {
		t.Error("a path and a bare name resolve differently")
	}
}

// TestAnUnknownNameStillGetsAGlyph is the invariant the column rests on: an
// empty cell would shift every name it prefixes by one, so the column would
// stop lining up.
func TestAnUnknownNameStillGetsAGlyph(t *testing.T) {
	for _, name := range []string{"data.bin", "noextension", "", ".weird", "archive.qqq"} {
		if got := For(name); got == "" {
			t.Errorf("For(%q) returned nothing", name)
		}
	}
	if got := For("data.bin"); got != theme.IconFile {
		t.Errorf("For(data.bin) = %q, want the generic file glyph", got)
	}
}

// TestNoGlyphIsEmptyOrCarriesStyling walks both tables. An empty value is the
// bug above written into the table instead of returned; an escape sequence
// would be Rule 122's — a cell is measured while plain, and colour goes
// through Style.
func TestNoGlyphIsEmptyOrCarriesStyling(t *testing.T) {
	check := func(table map[string]string, which string) {
		for key, glyph := range table {
			if glyph == "" {
				t.Errorf("%s[%q] is empty", which, key)
			}
			if strings.Contains(glyph, "\x1b") {
				t.Errorf("%s[%q] carries an ANSI escape; colour belongs in Style (Rule 122)", which, key)
			}
		}
	}
	check(basenames, "basenames")
	check(extensions, "extensions")
}

// TestEveryExtensionKeyStartsWithADot catches the typo that makes an entry
// unreachable: filepath.Ext returns ".go", so a key written "go" never matches
// and the file silently falls through to the generic glyph.
func TestEveryExtensionKeyStartsWithADot(t *testing.T) {
	for key := range extensions {
		if !strings.HasPrefix(key, ".") {
			t.Errorf("extensions[%q] can never match — filepath.Ext returns a leading dot", key)
		}
	}
}

// TestEveryTableKeyIsLowercase catches the other unreachable entry: the lookup
// lowercases the name first, so a key with a capital in it is dead.
func TestEveryTableKeyIsLowercase(t *testing.T) {
	for _, table := range []map[string]string{basenames, extensions} {
		for key := range table {
			if key != strings.ToLower(key) {
				t.Errorf("%q can never match — the lookup lowercases the name first", key)
			}
		}
	}
}
