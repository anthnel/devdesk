package scan

import (
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
func FixCommand(ecosystem, pkg, version string) (string, bool) {
	if pkg == "" || version == "" {
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

// CompareVersions orders two versions numerically, returning -1, 0 or 1 and
// true, or false when either has no leading number — a distro epoch such as
// "1:1.1.1n", a commit hash, a name. A missing component reads as zero
// (1.2 == 1.2.0), and a version with a "-" suffix sorts below the bare one
// (1.2.3-rc1 < 1.2.3). The result is a lenient ordering, not semver.
func CompareVersions(a, b string) (int, bool) {
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
			if c, _ := CompareVersions(opt, best); c >= 0 {
				continue
			}
		default:
			continue
		}
		best, bestSameMajor = opt, same
	}
	return best
}
