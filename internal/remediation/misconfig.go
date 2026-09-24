package remediation

import (
	"regexp"
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
	// Target is the file the fix edits, when it is not the one the finding
	// points at: a slash path relative to root, the scanned directory, or a
	// reason when there is no file to edit. Nil edits f.File.
	//
	// It exists for the build context (§3.81), whose finding points at the
	// Dockerfile line that copies everything while the fix belongs in the
	// .dockerignore beside it. It may read the disk, so it is called where the
	// fix is computed, never where the shortcut column is.
	Target func(root string, f scan.Finding) (rel, reason string)
}

// FileFor returns the file this rule's fix edits for f, relative to root, or
// the reason there is none.
func (r Rule) FileFor(root string, f scan.Finding) (rel, reason string) {
	if r.Target == nil {
		return f.File, ""
	}
	return r.Target(root, f)
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
	// ReasonNoAptGetInstall fires when the reported lines no longer contain an
	// apt-get install — the file changed since the finding was reported. The
	// package-manager siblings below are the same reason for their own command.
	ReasonNoAptGetInstall   = "The reported lines do not contain an apt-get install"
	ReasonNoApkAdd          = "The reported lines do not contain an apk add"
	ReasonNoYumInstall      = "The reported lines do not contain a yum install"
	ReasonNoDnfInstall      = "The reported lines do not contain a dnf install"
	ReasonNoMicrodnfInstall = "The reported lines do not contain a microdnf install"
	ReasonNoZypperInstall   = "The reported lines do not contain a zypper install"
	// ReasonNotAnAddInstruction and ReasonAddExtractsOrFetches guard
	// fixAddInsteadOfCopy: the first refuses a stale line reference, the second
	// refuses an ADD that COPY genuinely cannot replace.
	ReasonNotAnAddInstruction  = "The reported line is not an ADD instruction"
	ReasonAddExtractsOrFetches = "This ADD extracts an archive or fetches a URL, which COPY cannot do"
	// ReasonNotACopyInstruction and ReasonCopyHasTooFewArgs guard
	// fixCopyMissingTrailingSlash the same way.
	ReasonNotACopyInstruction = "The reported line is not a COPY instruction"
	ReasonCopyHasTooFewArgs   = "This COPY has two or fewer arguments, so the destination is unambiguous without a trailing slash"
	// ReasonNotAMaintainerInstruction and ReasonMaintainerContinues guard
	// fixMaintainerDeprecated.
	ReasonNotAMaintainerInstruction = "The reported line is not a MAINTAINER instruction"
	ReasonMaintainerContinues       = "The MAINTAINER instruction continues onto another line"
	// ReasonNoDockerignore, ReasonTwoIgnoreFiles and ReasonGitAlreadyIgnored
	// guard fixIgnoreGit (§3.81). DevDesk adds to an ignore file; it does not
	// write one, since what belongs in an image is a policy it would be
	// inventing.
	ReasonNoDockerignore    = "There is no .dockerignore to add .git to — DevDesk does not create one"
	ReasonTwoIgnoreFiles    = "Two ignore files may apply, depending on the builder — add .git to the one yours reads"
	ReasonGitAlreadyIgnored = "The .dockerignore already excludes .git"
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
// It stays short on purpose. Every entry is a textual edit whose correctness
// does not depend on guessing what the image is for — a flag insertion, an
// append, or a keyword swap that Trivy's own check already proved is safe by
// the shape of what it flagged. The candidates not in it were each rejected
// for a stated reason:
//
//   - a missing HEALTHCHECK (AVD-DS-0026) has no universal command — what to
//     probe depends on what the image runs, which the Dockerfile does not say;
//   - a `:latest` base image (AVD-DS-0001) is already the Remediation tab's job
//     (§3.2), which resolves real tags from the registry rather than inventing
//     one, and a second path to the same edit could only disagree with the
//     first;
//   - `apt-get dist-upgrade` (AVD-DS-0024) is deprecated in Trivy's own check
//     set, so a Dockerfile stops being flagged for it on its own;
//   - a self-referencing `COPY --from` (0006), a duplicate FROM alias (0012), an
//     EXPOSE port out of range (0008) or set to 22 (0004), `RUN cd` instead of
//     WORKDIR (0013), both wget and curl in use (0014), and a package-manager
//     `update` with no matching `install` in the same RUN (0017) are all real
//     defects or real decisions, and none of them names what the fix should be —
//     only the Dockerfile's author knows the other stage, the intended port, the
//     tool to keep, or the packages to install;
//   - duplicate ENTRYPOINT/CMD/HEALTHCHECK (0007, 0016, 0023), an unabsolute
//     WORKDIR (0009) and `sudo` in a RUN (0010) are each safe in principle —
//     only the last instruction of its kind takes effect, WORKDIR can be
//     resolved against the stage's own chain, and `sudo` is a no-op before any
//     USER switch — but none of that is tracked by this package yet: it reads
//     FROM, ARG and (for these fixes) the raw line range Trivy reports, not a
//     stage's running instruction history. Candidates for a later pass once
//     that tracking exists, not for this one.
var catalog = map[string]Rule{
	RuleKey("AVD-DS-0002"): {
		AVDID: "AVD-DS-0002",
		Title: "Add a USER instruction to the final stage",
		Fix:   fixRootUser,
	},
	RuleKey("AVD-DS-0005"): {
		AVDID: "AVD-DS-0005",
		Title: "Replace ADD with COPY",
		Fix:   fixAddInsteadOfCopy,
	},
	RuleKey("AVD-DS-0011"): {
		AVDID: "AVD-DS-0011",
		Title: "Add a trailing slash to the COPY destination",
		Fix:   fixCopyMissingTrailingSlash,
	},
	RuleKey("AVD-DS-0015"): {
		AVDID: "AVD-DS-0015",
		Title: "Add yum clean all after yum install",
		Fix:   fixYumClean,
	},
	RuleKey("AVD-DS-0019"): {
		AVDID: "AVD-DS-0019",
		Title: "Add dnf clean all after dnf install",
		Fix:   fixDnfClean,
	},
	RuleKey("AVD-DS-0020"): {
		AVDID: "AVD-DS-0020",
		Title: "Add zypper clean after zypper install",
		Fix:   fixZypperClean,
	},
	RuleKey("AVD-DS-0021"): {
		AVDID: "AVD-DS-0021",
		Title: "Add -y to apt-get install",
		Fix:   fixAptGetAssumeYes,
	},
	RuleKey("AVD-DS-0022"): {
		AVDID: "AVD-DS-0022",
		Title: "Replace MAINTAINER with LABEL",
		Fix:   fixMaintainerDeprecated,
	},
	RuleKey("AVD-DS-0025"): {
		AVDID: "AVD-DS-0025",
		Title: "Add --no-cache to apk add",
		Fix:   fixApkNoCache,
	},
	RuleKey("AVD-DS-0027"): {
		AVDID: "AVD-DS-0027",
		Title: "Add microdnf clean all after microdnf install",
		Fix:   fixMicrodnfClean,
	},
	RuleKey("AVD-DS-0029"): {
		AVDID: "AVD-DS-0029",
		Title: "Add --no-install-recommends to apt-get install",
		Fix:   fixAptGetNoRecommends,
	},
	// Kubernetes manifests (§3.80) — see k8s.go for what is in and what is
	// left out, and why.
	RuleKey("KSV-0001"): {
		AVDID: "KSV-0001",
		Title: "Set allowPrivilegeEscalation: false on the container",
		Fix:   fixPrivilegeEscalation,
	},
	RuleKey("KSV-0017"): {
		AVDID: "KSV-0017",
		Title: "Set privileged: false on the container",
		Fix:   fixPrivileged,
	},
	// The build context (§3.81): the one fix of a finding DevDesk emits itself,
	// and the one whose edit lands in another file than the finding's.
	RuleKey(scan.BuildContextGitID): {
		AVDID:  scan.BuildContextGitID,
		Title:  "Add .git to .dockerignore",
		Fix:    fixIgnoreGit,
		Target: ignoreFileToEdit,
	},
	RuleKey(scan.K8sAPIRemovedID): {
		AVDID: scan.K8sAPIRemovedID,
		Title: "Move the resource to the apiVersion that replaced it",
		Fix:   fixRemovedAPI,
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

// recommendsFlag is what fixAptGetNoRecommends adds. Its position relative to
// "install" does not matter to apt-get, so the fix always puts it in the same
// place — right after "install" — rather than trying to match wherever a
// human might have put the rest of the command's own flags.
const recommendsFlag = "--no-install-recommends"

// aptGetInstall matches an apt-get install invocation. It does not try to
// mirror the full breadth of Trivy's own check (flags between "apt-get" and
// "install" are rare enough to skip), because a match here only has to find
// what to fix, not decide what Trivy would flag — that decision was already
// made when the finding was reported.
var aptGetInstall = regexp.MustCompile(`apt-get\s+install\b`)

// apkAdd matches an apk add invocation, on the same terms as aptGetInstall.
var apkAdd = regexp.MustCompile(`apk\s+add\b`)

// assumeYesFlag recognizes apt-get's confirmation flag in any of the forms
// apt-get itself accepts: a short flag cluster containing 'y' (`-y`, `-qy`,
// `-yq`), or the long spellings. A literal `strings.Contains(s, "-y")` — good
// enough for the long, unique --no-install-recommends — would miss `-qy`.
var assumeYesFlag = regexp.MustCompile(`(^|\s)-[A-Za-z]*y[A-Za-z]*(\s|$)|--yes\b|--assume-yes\b`)

// fixCommandFlag is fixAptGetNoRecommends, fixAptGetAssumeYes and
// fixApkNoCache's shared shape: find every invocation of the command the
// finding's line range covers, and add insertText right after it unless
// hasFlag already reports the flag present in that invocation's own
// statement.
//
// It only looks at the reported span, not the whole file: an invocation
// elsewhere in the Dockerfile is a different finding, and fixing it here
// would be an edit the diff never explained. Within the span, each invocation
// is checked on its own — a RUN chaining two of them with `&&` where only one
// already has the flag is not "already fixed" just because the flag appears
// somewhere in the line.
func fixCommandFlag(content []byte, f scan.Finding, command *regexp.Regexp, hasFlag func(string) bool, insertText, noMatchReason string) ([]patch.Edit, string) {
	if !dockerfile.IsDockerfileName(baseName(f.File)) {
		return nil, ReasonNotADockerfile
	}
	span, ok := lineSpan(content, f.Line, f.EndLine)
	if !ok {
		return nil, ReasonUnreadableSpan
	}
	text := string(content[span.Start:span.End])

	matches := command.FindAllStringIndex(text, -1)
	if matches == nil {
		return nil, noMatchReason
	}

	var edits []patch.Edit
	for _, m := range matches {
		stmtEnd := statementEnd(text, m[1])
		if hasFlag(text[m[1]:stmtEnd]) {
			continue
		}
		at := span.Start + m[1]
		edits = append(edits, patch.Edit{Span: patch.Span{Start: at, End: at}, New: insertText})
	}
	if len(edits) == 0 {
		return nil, ReasonAlreadyFixed
	}
	return edits, ""
}

// fixAptGetNoRecommends satisfies "'apt-get' missing '--no-install-recommends'"
// (AVD-DS-0029).
func fixAptGetNoRecommends(content []byte, f scan.Finding) ([]patch.Edit, string) {
	return fixCommandFlag(content, f, aptGetInstall,
		func(s string) bool { return strings.Contains(s, recommendsFlag) },
		" "+recommendsFlag, ReasonNoAptGetInstall)
}

// fixAptGetAssumeYes satisfies "'apt-get' missing '-y'" (AVD-DS-0021).
func fixAptGetAssumeYes(content []byte, f scan.Finding) ([]patch.Edit, string) {
	return fixCommandFlag(content, f, aptGetInstall, assumeYesFlag.MatchString, " -y", ReasonNoAptGetInstall)
}

// fixApkNoCache satisfies "'apk add' is missing '--no-cache'" (AVD-DS-0025).
func fixApkNoCache(content []byte, f scan.Finding) ([]patch.Edit, string) {
	return fixCommandFlag(content, f, apkAdd,
		func(s string) bool { return strings.Contains(s, "--no-cache") },
		" --no-cache", ReasonNoApkAdd)
}

// yumInstall, dnfInstall, microdnfInstall and zypperInstall match the install
// invocation fixAppendCleanup looks for, one per package manager.
var (
	yumInstall      = regexp.MustCompile(`yum\s+install\b`)
	dnfInstall      = regexp.MustCompile(`dnf\s+install\b`)
	microdnfInstall = regexp.MustCompile(`microdnf\s+install\b`)
	zypperInstall   = regexp.MustCompile(`zypper\s+install\b`)
)

// fixAppendCleanup is fixYumClean, fixDnfClean, fixMicrodnfClean and
// fixZypperClean's shared shape: Trivy's own check for each of these only
// counts a cleanup command as satisfying it when it is the last thing the RUN
// does, so the fix appends cleanCmd at the very end of the reported span
// rather than placing it next to the install call — anywhere else, a
// re-scan would still flag it.
//
// It is idempotent against the one case worth checking without a full shell
// parse: the span, once trailing whitespace and a stray continuation
// backslash are trimmed, already ending in cleanCmd.
func fixAppendCleanup(content []byte, f scan.Finding, install *regexp.Regexp, cleanCmd, noMatchReason string) ([]patch.Edit, string) {
	if !dockerfile.IsDockerfileName(baseName(f.File)) {
		return nil, ReasonNotADockerfile
	}
	span, ok := lineSpan(content, f.Line, f.EndLine)
	if !ok {
		return nil, ReasonUnreadableSpan
	}
	text := string(content[span.Start:span.End])
	if !install.MatchString(text) {
		return nil, noMatchReason
	}

	trimmed := strings.TrimRight(text, " \t\r\n")
	trimmed = strings.TrimRight(strings.TrimSuffix(trimmed, "\\"), " \t")
	if strings.HasSuffix(trimmed, cleanCmd) {
		return nil, ReasonAlreadyFixed
	}
	at := span.Start + len(trimmed)
	return []patch.Edit{{Span: patch.Span{Start: at, End: at}, New: " && " + cleanCmd}}, ""
}

// fixYumClean satisfies "'yum clean all' missing" (AVD-DS-0015).
func fixYumClean(content []byte, f scan.Finding) ([]patch.Edit, string) {
	return fixAppendCleanup(content, f, yumInstall, "yum clean all", ReasonNoYumInstall)
}

// fixDnfClean satisfies "'dnf clean all' missing" (AVD-DS-0019).
func fixDnfClean(content []byte, f scan.Finding) ([]patch.Edit, string) {
	return fixAppendCleanup(content, f, dnfInstall, "dnf clean all", ReasonNoDnfInstall)
}

// fixMicrodnfClean satisfies "'microdnf clean all' missing" (AVD-DS-0027).
func fixMicrodnfClean(content []byte, f scan.Finding) ([]patch.Edit, string) {
	return fixAppendCleanup(content, f, microdnfInstall, "microdnf clean all", ReasonNoMicrodnfInstall)
}

// fixZypperClean satisfies "'zypper clean' missing" (AVD-DS-0020). zypper's
// own cleanup subcommand takes no second word, unlike the three above.
func fixZypperClean(content []byte, f scan.Finding) ([]patch.Edit, string) {
	return fixAppendCleanup(content, f, zypperInstall, "zypper clean", ReasonNoZypperInstall)
}

// addUnsafe are the substrings that make an ADD instruction do something COPY
// cannot: extract a local tar archive, or fetch from a URL. Trivy's own check
// already excludes every ADD carrying one of these from AVD-DS-0005, so a
// finding reaching this fix is one it has already proven is a plain copy —
// this re-checks the live file rather than trusting a finding computed from a
// version of it that may have since changed.
var addUnsafe = []string{".tar", "http://", "https://", "git@"}

// fixAddInsteadOfCopy satisfies "ADD instead of COPY" (AVD-DS-0005) by
// swapping the instruction's own keyword. Every argument is left untouched:
// COPY and ADD take the same syntax for a plain file copy, which is the only
// case this fix ever sees.
func fixAddInsteadOfCopy(content []byte, f scan.Finding) ([]patch.Edit, string) {
	if !dockerfile.IsDockerfileName(baseName(f.File)) {
		return nil, ReasonNotADockerfile
	}
	lines := contentLines(content)
	if f.Line < 1 || f.Line > len(lines) {
		return nil, ReasonUnreadableSpan
	}
	first := lines[f.Line-1]
	if instruction(first.text) != "ADD" {
		return nil, ReasonNotAnAddInstruction
	}
	span, ok := lineSpan(content, f.Line, f.EndLine)
	if !ok {
		return nil, ReasonUnreadableSpan
	}
	text := string(content[span.Start:span.End])
	for _, unsafe := range addUnsafe {
		if strings.Contains(text, unsafe) {
			return nil, ReasonAddExtractsOrFetches
		}
	}

	kw, ok := keywordSpan(first)
	if !ok {
		return nil, ReasonUnreadableSpan
	}
	old := first.text[kw.Start-first.off : kw.End-first.off]
	return []patch.Edit{{Span: kw, Old: old, New: "COPY"}}, ""
}

// fixCopyMissingTrailingSlash satisfies "COPY with more than two arguments
// not ending with slash" (AVD-DS-0011) by appending "/" to the destination —
// the last argument, since COPY only accepts a directory as its destination
// once it has more than one source.
func fixCopyMissingTrailingSlash(content []byte, f scan.Finding) ([]patch.Edit, string) {
	if !dockerfile.IsDockerfileName(baseName(f.File)) {
		return nil, ReasonNotADockerfile
	}
	lines := contentLines(content)
	if f.Line < 1 || f.Line > len(lines) {
		return nil, ReasonUnreadableSpan
	}
	if instruction(lines[f.Line-1].text) != "COPY" {
		return nil, ReasonNotACopyInstruction
	}
	span, ok := lineSpan(content, f.Line, f.EndLine)
	if !ok {
		return nil, ReasonUnreadableSpan
	}
	text := string(content[span.Start:span.End])

	fields := strings.Fields(stripContinuations(text))
	var nonFlag []string
	for _, a := range fields[1:] { // fields[0] is the COPY keyword itself
		if !strings.HasPrefix(a, "--") {
			nonFlag = append(nonFlag, a)
		}
	}
	if len(nonFlag) <= 2 {
		return nil, ReasonCopyHasTooFewArgs
	}
	if strings.HasSuffix(nonFlag[len(nonFlag)-1], "/") {
		return nil, ReasonAlreadyFixed
	}

	trimmed := strings.TrimRight(text, " \t\r\n")
	at := span.Start + len(trimmed)
	return []patch.Edit{{Span: patch.Span{Start: at, End: at}, New: "/"}}, ""
}

// fixMaintainerDeprecated satisfies "Deprecated MAINTAINER used"
// (AVD-DS-0022) by rewriting the whole instruction as the LABEL Docker itself
// has documented as the replacement since 1.13.0.
func fixMaintainerDeprecated(content []byte, f scan.Finding) ([]patch.Edit, string) {
	if !dockerfile.IsDockerfileName(baseName(f.File)) {
		return nil, ReasonNotADockerfile
	}
	lines := contentLines(content)
	if f.Line < 1 || f.Line > len(lines) {
		return nil, ReasonUnreadableSpan
	}
	l := lines[f.Line-1]
	if instruction(l.text) != "MAINTAINER" {
		return nil, ReasonNotAMaintainerInstruction
	}
	if strings.HasSuffix(strings.TrimRight(l.text, " \t"), "\\") {
		return nil, ReasonMaintainerContinues
	}

	sep := strings.IndexAny(l.text, " \t")
	if sep < 0 {
		return nil, ReasonUnreadableSpan
	}
	value := strings.TrimSpace(l.text[sep+1:])
	value = strings.Trim(value, `"'`)
	value = strings.ReplaceAll(value, `"`, `\"`)

	old := l.text
	return []patch.Edit{{
		Span: patch.Span{Start: l.off, End: l.off + len(l.text)},
		Old:  old,
		New:  `LABEL maintainer="` + value + `"`,
	}}, ""
}

// statementEnd finds where the shell statement starting at from ends: the next
// `&&` or `;`, or the end of text when neither separates it from what follows.
// A RUN chaining several commands is one Dockerfile instruction but several
// statements, and the flag has to be checked against the one that matched, not
// against whatever a later statement in the same RUN happens to contain.
func statementEnd(text string, from int) int {
	end := len(text)
	for _, sep := range []string{"&&", ";"} {
		if i := strings.Index(text[from:], sep); i >= 0 && from+i < end {
			end = from + i
		}
	}
	return end
}

// lineSpan returns the byte range covering lines startLine..endLine (1-based,
// inclusive) of content. endLine before startLine — including zero, which is
// what a finding cached before EndLine was recorded carries — is treated as a
// single-line span rather than declined outright: most apt-get installs are
// one physical line, and a finding that predates EndLine still names one.
func lineSpan(content []byte, startLine, endLine int) (patch.Span, bool) {
	if endLine < startLine {
		endLine = startLine
	}
	lines := contentLines(content)
	if startLine < 1 || startLine > len(lines) || endLine > len(lines) {
		return patch.Span{}, false
	}
	last := lines[endLine-1]
	return patch.Span{Start: lines[startLine-1].off, End: last.off + len(last.text)}, true
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

// keywordSpan returns the byte span of a line's leading token — its
// instruction keyword, exactly as written, case included. Unlike
// instruction(), which uppercases for comparison, a caller replacing the
// keyword needs the real span to build a patch.Edit whose Old matches what
// Rewrite will find there.
func keywordSpan(l contentLine) (patch.Span, bool) {
	start := 0
	for start < len(l.text) && (l.text[start] == ' ' || l.text[start] == '\t') {
		start++
	}
	end := start
	for end < len(l.text) && l.text[end] != ' ' && l.text[end] != '\t' {
		end++
	}
	if start == end {
		return patch.Span{}, false
	}
	return patch.Span{Start: l.off + start, End: l.off + end}, true
}

// stripContinuations turns a backslash immediately followed by a newline —
// Dockerfile's line-continuation marker — into a space, so a multi-line
// instruction tokenizes as the one shell command it is instead of leaving the
// backslash as a stray token of its own.
func stripContinuations(text string) string {
	text = strings.ReplaceAll(text, "\\\r\n", " ")
	return strings.ReplaceAll(text, "\\\n", " ")
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
