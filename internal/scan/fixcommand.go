package scan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Class values Trivy reports for a vulnerability result (Result.Class).
const (
	// ClassOSPackages marks a package installed by the base image's package
	// manager. A base image bump can clear it.
	ClassOSPackages = "os-pkgs"
	// ClassLangPackages marks an application dependency. Only a bump of the
	// dependency itself clears it; a new base image changes nothing.
	ClassLangPackages = "lang-pkgs"
)

// FixCommand returns the command that moves pkg to version in the given
// ecosystem (Trivy's Result.Type), and false when no command is known — an
// ecosystem outside the table, or nothing to move to. It never invents one:
// "no command" is a real answer, and the caller shows the target version alone.
//
// pkg and version come from a scan, so from whoever published the package: a
// command is built only from values that pass plainToken, and none otherwise.
// The command is meant to be pasted into a shell.
func FixCommand(ecosystem, pkg, version string) (string, bool) {
	if !plainToken(pkg) || !plainToken(version) {
		return "", false
	}
	switch ecosystem {
	case "gomod", "gobinary":
		return "go get " + pkg + "@v" + strings.TrimPrefix(version, "v"), true
	case "npm", "node-pkg":
		return "npm install " + pkg + "@" + version, true
	case "yarn":
		return "yarn add " + pkg + "@" + version, true
	case "pnpm":
		return "pnpm add " + pkg + "@" + version, true
	case "pip", "pipenv", "python-pkg":
		return "pip install " + pkg + "==" + version, true
	case "poetry":
		return "poetry add " + pkg + "==" + version, true
	case "cargo", "rust-binary":
		return "cargo update -p " + pkg + " --precise " + version, true
	case "alpine":
		return "apk upgrade " + pkg, true
	case "debian", "ubuntu":
		return "apt-get install --only-upgrade " + pkg, true
	}
	return "", false
}

// plainToken reports whether s is a package name or a version a command can be
// built from: one or more of A-Z a-z 0-9 @ / . _ + ~ : = ^ , -, and not an
// option (no leading "-"). That is every spelling the ecosystems above produce
// (scoped names, Go module paths, Debian epochs and revisions, "+build"
// metadata); a space, a quote or any shell metacharacter is not one of them.
func plainToken(s string) bool {
	if s == "" || s[0] == '-' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("@/._+~:=^,-", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// versionNumbers matches the leading dotted number of a version, ignoring a
// "v" prefix. What follows (a pre-release, a distro revision such as -r0 or
// +deb11u3) is not part of the comparison, except as noted in CompareVersions.
var versionNumbers = regexp.MustCompile(`^v?(\d+(?:\.\d+)*)`)

// parseVersion returns the dotted numbers of v and the text after them.
func parseVersion(v string) (nums []int, rest string, ok bool) {
	v = strings.TrimSpace(v)
	m := versionNumbers.FindStringSubmatch(v)
	if m == nil {
		return nil, "", false
	}
	for _, part := range strings.Split(m[1], ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, "", false
		}
		nums = append(nums, n)
	}
	rest = v[len(m[0]):]
	if strings.HasPrefix(rest, ":") {
		// A Debian epoch ("1:1.1.1n"): the number before the colon outranks
		// everything after it, so reading it as a version would misorder.
		return nil, "", false
	}
	return nums, rest, true
}

// suffixLetter returns the lowercase letter that directly follows the digits of
// a version when it is a release letter (OpenSSL style: 1.1.1w), 0 otherwise.
// The letter has to be alone: it is followed by nothing or by "-", "+" or "~",
// so a word ("rc1") or a letter and a number ("a1") is not one.
func suffixLetter(rest string) byte {
	if rest == "" {
		return 0
	}
	c := rest[0]
	if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
		return 0
	}
	if len(rest) > 1 && strings.IndexByte("-+~", rest[1]) < 0 {
		return 0
	}
	return c | 0x20
}

// CompareVersions orders two versions numerically, returning -1, 0 or 1 and
// true, or false when either has no leading number — a distro epoch such as
// "1:1.1.1n", a commit hash, a name. A missing component reads as zero
// (1.2 == 1.2.0), and a version with a "-" suffix sorts below the bare one
// (1.2.3-rc1 < 1.2.3). A single release letter right after the digits is an
// order too, as OpenSSL writes it: 1.1.1 < 1.1.1l < 1.1.1w. The result is a
// lenient ordering, not semver. CompareVersions knows no ecosystem: use
// CompareVersionsIn when the versions belong to one.
func CompareVersions(a, b string) (int, bool) {
	return CompareVersionsIn("", a, b)
}

// pep440 are the Trivy ecosystems whose versions follow PEP 440.
var pep440 = map[string]bool{"pip": true, "pipenv": true, "poetry": true, "python-pkg": true}

// CompareVersionsIn is CompareVersions for versions of ecosystem (Trivy's
// Result.Type). A lone letter after the digits is an increasing release letter
// in most of them (OpenSSL: 1.1.1l < 1.1.1w) but a pre-release in PEP 440
// ("2.0b" is older than "2.0"): there it is not an order, and a version that
// carries one cannot be compared (false).
func CompareVersionsIn(ecosystem, a, b string) (int, bool) {
	an, ar, aok := parseVersion(a)
	bn, br, bok := parseVersion(b)
	if !aok || !bok {
		return 0, false
	}
	for i := 0; i < max(len(an), len(bn)); i++ {
		var x, y int
		if i < len(an) {
			x = an[i]
		}
		if i < len(bn) {
			y = bn[i]
		}
		if x != y {
			if x < y {
				return -1, true
			}
			return 1, true
		}
	}
	al, bl := suffixLetter(ar), suffixLetter(br)
	if pep440[ecosystem] && (al != 0 || bl != 0) {
		return 0, false
	}
	if al != bl {
		if al < bl {
			return -1, true
		}
		return 1, true
	}
	aPre, bPre := strings.HasPrefix(ar, "-"), strings.HasPrefix(br, "-")
	switch {
	case aPre && !bPre:
		return -1, true
	case !aPre && bPre:
		return 1, true
	}
	return 0, true
}

// PickFixed chooses the version to move to from Trivy's FixedVersion, which
// lists one fixed version per maintained branch ("1.2.4, 1.3.1"). It takes the
// lowest one on the installed version's major line, which is the smallest
// change that clears the CVE, and falls back to the lowest overall when no
// branch shares the major. A single value is returned as is, comparable or
// not; several that cannot be compared give "", because picking one of them
// would be a guess.
func PickFixed(installed, fixedIn string) string {
	return PickFixedIn("", installed, fixedIn)
}

// PickFixedIn is PickFixed for a vulnerability of ecosystem, ordering the
// candidates with CompareVersionsIn.
func PickFixedIn(ecosystem, installed, fixedIn string) string {
	var options []string
	for _, v := range strings.Split(fixedIn, ",") {
		if v = strings.TrimSpace(v); v != "" {
			options = append(options, v)
		}
	}
	switch len(options) {
	case 0:
		return ""
	case 1:
		return options[0]
	}

	instNums, _, instOK := parseVersion(installed)
	best, bestSameMajor := "", false
	for _, opt := range options {
		nums, _, ok := parseVersion(opt)
		if !ok {
			return ""
		}
		same := instOK && len(instNums) > 0 && nums[0] == instNums[0]
		switch {
		case best == "":
		case same && !bestSameMajor:
		case same == bestSameMajor:
			c, ok := CompareVersionsIn(ecosystem, opt, best)
			if !ok {
				return ""
			}
			if c >= 0 {
				continue
			}
		default:
			continue
		}
		best, bestSameMajor = opt, same
	}
	return best
}

// fixInstruction is the sentence a finding carries when its ecosystem has no
// known command. The text is stored in the scan cache, so it must not change.
func fixInstruction(pkg, target string) string {
	return fmt.Sprintf("Update %s to %s", pkg, target)
}

// IsFixInstruction reports whether f.FixCommand is the sentence fixInstruction
// writes for f's package rather than a command the user can run. A caller shows
// a command as "Run: ..." and a sentence as plain text.
func IsFixInstruction(f Finding) bool {
	if f.PkgName == "" {
		return false
	}
	prefix := fixInstruction(f.PkgName, "")
	return strings.HasPrefix(f.FixCommand, prefix) && len(f.FixCommand) > len(prefix)
}
