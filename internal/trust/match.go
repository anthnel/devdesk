package trust

import (
	"path"
	"strings"
)

// Repository normalizes an image reference to the repository a rule matches:
// the registry host, then the path, without tag or digest. Docker Hub is
// spelled out — `python` is `docker.io/library/python` — so that
// `python:3.12` and `docker.io/library/python:3.12` fall under the same rule.
//
// It reads a reference the way remediation.ParseRef does, and a test holds the
// two in step: this package cannot import remediation, which imports scan,
// which implements this package's Verifier.
func Repository(ref string) string {
	s := strings.TrimSpace(ref)
	if before, _, ok := strings.Cut(s, "@"); ok {
		s = before
	}
	// A colon after the last slash is a tag; one before it is a registry port.
	if i := strings.LastIndex(s, ":"); i > strings.LastIndex(s, "/") {
		s = s[:i]
	}
	host, rest := "", s
	if first, after, ok := strings.Cut(s, "/"); ok &&
		(strings.ContainsAny(first, ".:") || first == "localhost") {
		host, rest = first, after
	}
	if host == "" || host == "docker.io" || host == "index.docker.io" {
		host = "docker.io"
		rest = strings.TrimPrefix(rest, "docker.io/")
		if !strings.Contains(rest, "/") {
			rest = "library/" + rest
		}
	}
	return host + "/" + rest
}

// Digest is the digest a reference is pinned by, "" when there is none.
func Digest(ref string) string {
	if _, digest, ok := strings.Cut(ref, "@"); ok {
		return digest
	}
	return ""
}

// matches reports whether a rule's pattern covers a normalized repository.
// `*` stays within one path segment, as in path.Match; a pattern ending in
// `/**` covers everything below its prefix, at any depth.
func matches(pattern, repo string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "/**"); ok {
		return strings.HasPrefix(repo, prefix+"/")
	}
	ok, err := path.Match(pattern, repo)
	return err == nil && ok
}

// validPattern refuses what path.Match cannot read, and a `**` anywhere but at
// the end: in the middle of a pattern it would silently mean one segment.
func validPattern(pattern string) bool {
	body := strings.TrimSuffix(pattern, "/**")
	if strings.Contains(body, "**") {
		return false
	}
	_, err := path.Match(body, "")
	return err == nil
}

// shadows reports a rule no image can reach because an earlier one already
// matches everything it would. Only the certain cases are reported — the same
// pattern, or a literal one the earlier pattern covers — so the warning is
// never wrong, only sometimes absent.
func shadows(earlier, later string) bool {
	if earlier == later {
		return true
	}
	if strings.ContainsAny(later, "*?[") {
		return false
	}
	return matches(earlier, later)
}

// Lookup finds the rule for a reference: the user's rules first, in file order,
// then the built-in ones. The first that matches wins.
func (p Policy) Lookup(ref string) (Rule, bool) {
	repo := Repository(ref)
	for _, rules := range [][]Rule{p.Rules, Builtin()} {
		for _, r := range rules {
			if matches(r.Match, repo) {
				return r, true
			}
		}
	}
	return Rule{}, false
}
