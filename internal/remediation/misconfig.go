package remediation

import (
	"strings"

	"github.com/anthnel/devdesk/internal/dockerfile"
	"github.com/anthnel/devdesk/internal/patch"
	"github.com/anthnel/devdesk/internal/scan"
)

// A misconfiguration is not fixed the way a vulnerability is (§3.78). Trivy
// reports the block it faults and a Resolution written in English — a sentence,
// not a replacement — so there is no general way to turn one into an edit.
//
// What there is instead is a **deliberately finite** catalog: the few rules that
// recur often enough to be worth writing by hand, each with the exact edit that
// satisfies it. A rule absent from the catalog is not a gap to be filled. It is
// the case the MCP server already covers, by handing a calling agent the span,
// the wording and the file (phase A) and re-measuring afterwards.
//
// That boundary is the point, and it is why Fix declines instead of
// approximating: an absence sends the user to something that works, while a
// doubtful edit sends them to a diff they have to second-guess.

// Rule is one misconfiguration this application knows how to fix on its own.
type Rule struct {
	// AVDID is the rule's identity as Trivy reports it in Finding.ID. It is
	// matched loosely — see ruleKey — because Trivy spells the same rule
	// "AVD-DS-0002" in one field and "DS002" in another.
	AVDID string
	// Title is what the UI calls the fix. It says what the edit does, not what
	// the rule forbids: the user is agreeing to a change, not reading an advisory.
	Title string
	// Fix returns the edits that satisfy the rule on content, or nil and a
	// reason when this instance is not one the catalog handles.
	//
	// It never guesses. An instance it cannot read exactly is one it declines,
	// and the reason is shown to the user as-is (Rule 129: English).
	Fix func(content []byte, f scan.Finding) ([]patch.Edit, string)
}

// The reasons a fix declines. They are constants because the view, the footer
// and the tests all read them, and a wording that drifts between the three is a
// wording that cannot be tested.
const (
	ReasonNoFixForRule   = "No built-in fix for this rule — use the MCP server and an agent"
	ReasonNotADockerfile = "The catalog only fixes Dockerfiles"
	ReasonNoStage        = "This Dockerfile declares no build stage"
	ReasonAlreadyFixed   = "The final stage already sets a user"
	ReasonUnreadableSpan = "The reported lines are not in this file"
	// ReasonUnknownBaseFamily is the one that matters: the account has to be
	// created before it can be used, and the command that creates it differs
	// between distributions. See fixRootUser.
	ReasonUnknownBaseFamily = "Cannot tell which distribution the final stage builds on, so the adduser flags cannot be chosen"
)

// The account the fix creates. The uid goes through an ARG so it can be
// overridden at build time without editing the file again, which is what
// `docker init` generates and what makes the value a default rather than a
// decision taken on the user's behalf.
const (
	nonRootUser = "appuser"
	nonRootUID  = "10001"
)

// catalog is keyed by ruleKey, and it is short on purpose. Adding an entry means
// having seen the rule recur and having an edit that is exactly right for it;
// anything less belongs to the agent path.
//
// Only one rule is in it today, and that is not an oversight. The other
// candidates were each rejected for a stated reason:
//
//   - a missing HEALTHCHECK has no universal command — what to probe depends on
//     what the image runs, which the Dockerfile does not say;
//   - a `:latest` base image is already the Remediation tab's job (§3.2), which
//     resolves real tags from the registry rather than inventing one, and a
//     second path to the same edit could only disagree with the first;
//   - ADD-instead-of-COPY and the apt-get rules are plausible entries whose AVD
//     ids could not be confirmed against a real Trivy run from here. A catalog
//     keyed by an id that is wrong matches nothing, silently, which is worse
//     than not offering the fix at all.
var catalog = map[string]Rule{
	RuleKey("AVD-DS-0002"): {
		AVDID: "AVD-DS-0002",
		Title: "Add a USER instruction to the final stage",
		Fix:   fixRootUser,
	},
}

// FixFor returns the catalog's rule for a finding, if it has one.
//
// It answers for misconfigurations only. A vulnerability, a secret or a licence
// reaching here is not an error — the tab is shared — it simply has no rule.
func FixFor(f scan.Finding) (Rule, bool) {
	if scan.Categorize(f) != scan.CategoryMisconfiguration {
		return Rule{}, false
	}
	r, ok := catalog[RuleKey(f.ID)]
	return r, ok
}

// RuleKey reduces the spellings of one rule id to a single key.
//
// Trivy writes "AVD-DS-0002" in AVDID and "DS002" in ID, and which of the two
// reaches a Finding has changed before. Matching on either spelling costs one
// function; matching on one of them costs a catalog that silently stops firing
// the day the other is used.
func RuleKey(id string) string {
	id = strings.ToUpper(strings.TrimSpace(id))
	id = strings.TrimPrefix(id, "AVD-")
	id = strings.ReplaceAll(id, "-", "")

	letters := strings.IndexFunc(id, func(r rune) bool { return r >= '0' && r <= '9' })
	if letters <= 0 {
		return id
	}
	prefix, digits := id[:letters], strings.TrimLeft(id[letters:], "0")
	if digits == "" {
		digits = "0"
	}
	return prefix + digits
}

// fixRootUser satisfies "specify at least 1 USER command" by creating an
// unprivileged account in the final stage and switching to it.
//
// **It creates the account rather than naming a uid.** A bare `USER 10001`
// switches to a uid that has no passwd entry: no name, no home, no shell. Much
// of what runs in a container asks the system who it is — anything calling
// getpwuid, a shell wanting $HOME, tools that write to a home directory — and
// all of it degrades in ways that surface later, at run time, far from this
// edit. `adduser` first, `USER appuser` after, is what `docker init` generates
// and what this now emits.
//
// **The flags are Debian's, so the base image has to be one.** The long options
// below belong to the Debian/Ubuntu `adduser`; busybox's, on Alpine, takes
// different ones (`-D`, `-H`, `-s /sbin/nologin`) and a distroless image has no
// shell to run either in. Guessing wrong produces a Dockerfile that fails at
// build, which is worse than offering nothing — so a base this cannot identify
// is declined, and the agent path (phase A) takes it.
//
// The block goes before the final stage's first CMD or ENTRYPOINT, which is
// where it has to be to apply to the process that runs — after it, it would
// change nothing. With neither, it goes at the end of the file.
//
// What it does not do is chown anything. A process that loses root may no longer
// be able to write where it used to, and no edit computed from the Dockerfile
// alone can know. That is a change the user reads in the diff and agrees to, and
// that the re-scan then confirms cleared the rule — it is not a claim that the
// image still works.
func fixRootUser(content []byte, f scan.Finding) ([]patch.Edit, string) {
	if !dockerfile.IsDockerfileName(baseName(f.File)) {
		return nil, ReasonNotADockerfile
	}
	stages := dockerfile.Parse(content).Stages
	if len(stages) == 0 {
		return nil, ReasonNoStage
	}

	final := stages[len(stages)-1]
	if !isDebianFamily(final.Image) {
		return nil, ReasonUnknownBaseFamily
	}

	lines := contentLines(content)
	if final.Line > len(lines) {
		return nil, ReasonUnreadableSpan
	}

	at, already := insertionPoint(lines, final.Line, len(content))
	if already {
		return nil, ReasonAlreadyFixed
	}

	text := addUserBlock(lineEnding(content))
	if at == len(content) && !endsWithNewline(content) {
		text = lineEnding(content) + text
	}
	return []patch.Edit{{Span: patch.Span{Start: at, End: at}, New: text}}, ""
}

// addUserBlock is the Debian/Ubuntu incantation, spelled out rather than
// condensed: every flag is there to make the account unusable for anything but
// running the process — no password, no home, no login shell.
func addUserBlock(eol string) string {
	lines := []string{
		"ARG UID=" + nonRootUID,
		"RUN adduser \\",
		`    --disabled-password \`,
		`    --gecos "" \`,
		`    --home "/nonexistent" \`,
		`    --shell "/usr/sbin/nologin" \`,
		"    --no-create-home \\",
		`    --uid "${UID}" \`,
		"    " + nonRootUser,
		"USER " + nonRootUser,
	}
	return strings.Join(lines, eol) + eol
}

// debianFamilyBases are the official images that build on Debian or Ubuntu
// unless their tag says otherwise.
//
// The list is short and conservative on purpose: an image missing from it is
// declined, which costs a fix, while an image wrongly in it emits a RUN that
// fails at build. A private base (`registry.example.com/base:1`) says nothing
// about its distribution and is therefore never accepted.
var debianFamilyBases = map[string]bool{
	"debian": true, "ubuntu": true,
	"golang": true, "node": true, "python": true, "ruby": true,
	"rust": true, "php": true, "perl": true, "openjdk": true,
	"maven": true, "gradle": true, "eclipse-temurin": true,
}

// isDebianFamily says whether ref's adduser takes Debian's flags.
//
// An "alpine" anywhere in the reference disqualifies it whatever the repository
// says — `golang:1.23-alpine` is busybox — and so does a digest with no tag,
// where nothing names the variant.
func isDebianFamily(ref string) bool {
	if ref == "" {
		return false
	}
	lower := strings.ToLower(ref)
	if strings.Contains(lower, "alpine") || strings.Contains(lower, "busybox") ||
		strings.Contains(lower, "distroless") || strings.Contains(lower, "scratch") {
		return false
	}

	repo := lower
	if i := strings.LastIndexAny(repo, ":@"); i >= 0 {
		repo = repo[:i]
	}
	// Only an official image, which is a bare name with no registry or user in
	// front of it. Anything namespaced is somebody's own build.
	if strings.Contains(repo, "/") {
		return false
	}
	return debianFamilyBases[repo]
}

// lineEnding returns what this file separates its lines with, so an inserted
// block does not mix LF into a CRLF file.
func lineEnding(content []byte) string {
	if i := indexByte(content, '\n'); i > 0 && content[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// insertionPoint finds where a USER instruction has to go in the stage that
// starts at the 1-based line finalFrom: just before that stage's first CMD or
// ENTRYPOINT, or at eof when it has neither.
//
// It also answers whether the stage already sets a user, because that is the
// same single pass — asking twice would let the two answers disagree about
// where the stage ends.
func insertionPoint(lines []contentLine, finalFrom, eof int) (at int, alreadySet bool) {
	for i := finalFrom; i < len(lines); i++ {
		switch instruction(lines[i].text) {
		case "USER":
			return 0, true
		case "CMD", "ENTRYPOINT":
			return lines[i].off, false
		}
	}
	return eof, false
}

// instruction returns the leading keyword of a Dockerfile line, uppercased, or
// "" for a blank line, a comment, or a continuation of the line before it.
//
// It does not follow continuations, and does not need to: it is only ever asked
// whether a line *starts* an instruction, and a continued line never does.
func instruction(text string) string {
	t := strings.TrimSpace(text)
	if t == "" || strings.HasPrefix(t, "#") {
		return ""
	}
	word, _, _ := strings.Cut(t, " ")
	word, _, _ = strings.Cut(word, "\t")
	return strings.ToUpper(word)
}

type contentLine struct {
	off  int
	text string
}

// contentLines cuts content into lines, keeping each one's byte offset so an
// edit can be placed at the start of one.
func contentLines(content []byte) []contentLine {
	var out []contentLine
	start := 0
	for i := 0; i <= len(content); i++ {
		if i < len(content) && content[i] != '\n' {
			continue
		}
		if i == len(content) && start == len(content) {
			break
		}
		out = append(out, contentLine{off: start, text: strings.TrimSuffix(string(content[start:i]), "\r")})
		start = i + 1
	}
	return out
}

func endsWithNewline(content []byte) bool {
	return len(content) > 0 && content[len(content)-1] == '\n'
}

// baseName is filepath.Base for a path Trivy reported, which always uses
// forward slashes whatever the host.
func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
