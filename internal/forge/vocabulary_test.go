package forge

import (
	"testing"
	"unicode"

	"github.com/anthnel/devdesk/internal/config"
)

// TestEveryConfiguredForgeHasItsOwnWords is the guard against a half-added
// forge: a `type:` the configuration view offers but no vocabulary answers to
// would render a screen with the wrong product's nouns on it, and nothing would
// say so.
func TestEveryConfiguredForgeHasItsOwnWords(t *testing.T) {
	seen := map[string]string{}
	for _, forgeType := range config.ForgeTypes() {
		v := VocabularyFor(forgeType)

		if v.Name == "" {
			t.Errorf("%q has no name", forgeType)
			continue
		}
		if previous, ok := seen[v.Name]; ok {
			t.Errorf("%q and %q both resolve to %q — one of them has no vocabulary of its own",
				previous, forgeType, v.Name)
		}
		seen[v.Name] = forgeType
	}
}

// TestEveryWordIsFilledIn — an empty field renders as nothing, which reads as a
// layout bug rather than as a missing translation. A struct with a hole in it
// is exactly what a table of words invites.
func TestEveryWordIsFilledIn(t *testing.T) {
	for _, forgeType := range config.ForgeTypes() {
		v := VocabularyFor(forgeType)
		words := map[string]string{
			"Name": v.Name, "Namespace": v.Namespace, "Namespaces": v.Namespaces,
			"Repository": v.Repository, "Repositories": v.Repositories,
			"ChangeRequest": v.ChangeRequest, "ChangeRequests": v.ChangeRequests,
			"ChangeRequestShort": v.ChangeRequestShort,
			"TokenLabel":         v.TokenLabel, "TokenPlaceholder": v.TokenPlaceholder,
			"TokenHelp": v.TokenHelp, "ExampleURL": v.ExampleURL,
		}
		for field, word := range words {
			if word == "" {
				t.Errorf("%s: %s is empty", forgeType, field)
			}
		}
	}
}

// TestANounIsCapitalisedAndItsPluralDiffers — the words are interpolated into
// titles and labels, so they carry their own capitalisation; and a plural equal
// to its singular means one of the two was copied rather than written.
func TestANounIsCapitalisedAndItsPluralDiffers(t *testing.T) {
	for _, forgeType := range config.ForgeTypes() {
		v := VocabularyFor(forgeType)
		pairs := [][3]string{
			{"Namespace", v.Namespace, v.Namespaces},
			{"Repository", v.Repository, v.Repositories},
			{"ChangeRequest", v.ChangeRequest, v.ChangeRequests},
		}
		for _, p := range pairs {
			if !unicode.IsUpper(rune(p[1][0])) {
				t.Errorf("%s: %s is %q, want it capitalised", forgeType, p[0], p[1])
			}
			if p[1] == p[2] {
				t.Errorf("%s: %s and its plural are both %q", forgeType, p[0], p[1])
			}
		}
	}
}

// TestAnUnknownForgeStillHasNouns — a screen with no nouns on it is a worse
// failure than a screen using the wrong forge's nouns, and applyDefaults
// already guarantees a known value. This is the belt to that brace.
func TestAnUnknownForgeStillHasNouns(t *testing.T) {
	v := VocabularyFor("bitbucket")
	if v.Name != VocabularyFor(config.ForgeGitLab).Name {
		t.Errorf("an unknown forge resolved to %q, want the GitLab fallback", v.Name)
	}
	if VocabularyFor("").Name == "" {
		t.Error("an empty forge type resolved to no vocabulary at all")
	}
}

// TestTheTwoForgesDoNotShareTheirNouns is what the table exists for: if GitLab
// and GitHub said the same words, every site that reads the vocabulary could
// have kept its literal.
func TestTheTwoForgesDoNotShareTheirNouns(t *testing.T) {
	gitlab := VocabularyFor(config.ForgeGitLab)
	github := VocabularyFor(config.ForgeGitHub)

	for _, p := range [][3]string{
		{"Namespace", gitlab.Namespace, github.Namespace},
		{"Repository", gitlab.Repository, github.Repository},
		{"ChangeRequest", gitlab.ChangeRequest, github.ChangeRequest},
		{"ChangeRequestShort", gitlab.ChangeRequestShort, github.ChangeRequestShort},
		{"ExampleURL", gitlab.ExampleURL, github.ExampleURL},
	} {
		if p[1] == p[2] {
			t.Errorf("%s is %q on both forges", p[0], p[1])
		}
	}
}
