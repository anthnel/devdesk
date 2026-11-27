package enginefixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"unicode/utf8"
)

// PlaceholderRun replaces the id of the contract run in a recorded fixture, so the
// resource names and the label value a contract made ("ddc-<run>-c1",
// "devdesk.test=<run>") read the same in every recording.
const PlaceholderRun = "fixture-run"

// Redaction is what the recorder knows about the machine and the run that produced
// an exchange: the text Redact has to replace. An empty field replaces nothing.
type Redaction struct {
	RunID            string            // the run id of contract.sh
	IDs              map[string]string // real 64-hex id -> role ("c1")
	Home, User, Host string            // of the recording user and machine
	// Public lists the 64-hex ids that may stay in a fixture (the digest of the pinned
	// image, an image id). Redact does not read it: UnknownEngineIDs does.
	Public []string
}

// PlaceholderID is the fixed id a role stands for in a fixture: 64 lower-case
// hexadecimal characters, so the validators of internal/docker accept it.
func PlaceholderID(role string) string {
	sum := sha256.Sum256([]byte("devdesk-fixture/" + role))
	return hex.EncodeToString(sum[:])
}

// Redact turns what a real engine answered into what a fixture may hold. It works
// on bytes (argv, stdout and stderr alike, a stream that is not UTF-8 included),
// edits nothing in its input, and leaves the exit code alone. In this order:
//
//  1. each full id of r.IDs, by the placeholder of its role (PlaceholderID);
//  2. the 12-character prefix of each id, as ps prints it, by the first 12
//     characters of that placeholder, only as a whole hexadecimal word;
//  3. the home directory, as a path prefix, by PlaceholderHome;
//  4. the run id, by PlaceholderRun;
//  5. the host name, by PlaceholderHost, as a whole word (a dash, a dot or a slash
//     separate words, as the local-identity rule reads a name); a host written as a
//     fully qualified name loses its internal suffix too (.home.arpa, .lan, .local,
//     .internal), or internal-name would refuse "fixture-host.lan", and the first
//     label of such a host is replaced on its own;
//  6. the user name, by PlaceholderUser, as a whole word.
//
// The host and the user are replaced only when LocalIdentityTerms keeps them: a
// name of two characters (dd, the lab user) or on the stop list (root) is not told
// apart by its text, and stays for CheckLeaks to refuse, which is the net that
// makes a recording fail instead of leaking. Host and user are matched whatever
// their case. Durations and timestamps are not normalised.
//
// The string is the "redacted" header of the fixture: the categories that replaced
// something (home, host, ids, run, user), sorted and joined by ", ", or "none".
func Redact(ex Exchange, r Redaction) (Exchange, string) {
	steps := redactSteps(r)
	used := map[string]bool{}
	apply := func(b []byte) []byte {
		for _, s := range steps {
			var changed bool
			if b, changed = s.run(b); changed {
				used[s.category] = true
			}
		}
		return b
	}

	out := Exchange{Exit: ex.Exit, Argv: make([]string, len(ex.Argv))}
	for i, word := range ex.Argv {
		out.Argv[i] = string(apply([]byte(word)))
	}
	out.Stdout = apply(bytes.Clone(ex.Stdout))
	out.Stderr = apply(bytes.Clone(ex.Stderr))

	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	if len(names) == 0 {
		return out, "none"
	}
	slices.Sort(names)
	return out, strings.Join(names, ", ")
}

// redactStep is one replacement of Redact: a term, what replaces it, the category
// the "redacted" header reports, and where in the text the term counts.
type redactStep struct {
	category string
	term     string
	repl     string
	fold     bool // match whatever the case (ASCII)
	boundary func(before, after []byte) bool
	// absorb, when set, returns how many bytes after a match go with it.
	absorb func(after []byte) int
}

func (s redactStep) run(b []byte) ([]byte, bool) {
	return replaceBounded(b, s)
}

// redactSteps lists the replacements in the order Redact applies them.
func redactSteps(r Redaction) []redactStep {
	var steps []redactStep
	ids := make([]string, 0, len(r.IDs))
	for id := range r.IDs {
		if id != "" {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		steps = append(steps, redactStep{category: "ids", term: id, repl: PlaceholderID(r.IDs[id])})
	}
	for _, id := range ids {
		if len(id) > shortIDLen {
			steps = append(steps, redactStep{
				category: "ids", term: id[:shortIDLen], repl: PlaceholderID(r.IDs[id])[:shortIDLen], boundary: hexWord,
			})
		}
	}
	if home := strings.TrimRight(r.Home, "/"); home != "" && home != PlaceholderHome {
		steps = append(steps, redactStep{category: "home", term: home, repl: PlaceholderHome, boundary: homePath})
	}
	if r.RunID != "" && r.RunID != PlaceholderRun {
		steps = append(steps, redactStep{category: "run", term: r.RunID, repl: PlaceholderRun})
	}
	for _, host := range hostTerms(r.Host) {
		steps = append(steps, redactStep{category: "host", term: host, repl: PlaceholderHost, fold: true, boundary: looseWord, absorb: internalSuffix})
	}
	for _, user := range LocalIdentityTerms(r.User) {
		steps = append(steps, redactStep{category: "user", term: user, repl: PlaceholderUser, fold: true, boundary: looseWord})
	}
	return steps
}

// shortIDLen is the length of the id ps prints.
const shortIDLen = 12

// hostTerms lists the names to look for: the host as given, and its first label when
// it is a fully qualified name, longest first, each only if LocalIdentityTerms keeps it.
func hostTerms(host string) []string {
	names := []string{host}
	if short, _, ok := strings.Cut(host, "."); ok {
		names = append(names, short)
	}
	return LocalIdentityTerms(names...)
}

// internalSuffixes are the zones internal-name refuses; a host name is not kept
// with one of them after the placeholder.
var internalSuffixes = []string{".home.arpa", ".lan", ".local", ".internal"}

// internalSuffix returns the length of the internal zone that starts after, or 0.
func internalSuffix(after []byte) int {
	lower := asciiLower(after[:min(len(after), len(".home.arpa")+1)])
	for _, suffix := range internalSuffixes {
		if !bytes.HasPrefix(lower, []byte(suffix)) {
			continue
		}
		if rest := lower[len(suffix):]; len(rest) == 0 || !isNameByte(rest[0]) || rest[0] == '/' {
			return len(suffix)
		}
	}
	return 0
}

// replaceBounded replaces each occurrence of the step's term in data by its
// replacement, where its boundary (nil: anywhere) accepts the bytes before and after
// it. data is not modified; the bool says whether anything was replaced, and then the
// result is a new slice.
func replaceBounded(data []byte, step redactStep) ([]byte, bool) {
	hay, needle, repl := data, []byte(step.term), step.repl
	if step.fold {
		hay, needle = asciiLower(data), asciiLower(needle)
	}
	var out []byte
	last, n := 0, 0
	for pos := 0; pos+len(needle) <= len(hay); {
		i := bytes.Index(hay[pos:], needle)
		if i < 0 {
			break
		}
		i += pos
		end := i + len(needle)
		if step.boundary != nil && !step.boundary(data[:i], data[end:]) {
			pos = i + 1
			continue
		}
		if step.absorb != nil {
			end += step.absorb(data[end:])
		}
		out = append(out, data[last:i]...)
		out = append(out, repl...)
		last, pos = end, end
		n++
	}
	if n == 0 {
		return data, false
	}
	return append(out, data[last:]...), true
}

// asciiLower lower-cases the ASCII letters byte by byte: the length never changes,
// whatever the bytes are (bytes.ToLower would rewrite an invalid UTF-8 byte).
func asciiLower(b []byte) []byte {
	out := bytes.Clone(b)
	for i, c := range out {
		if 'A' <= c && c <= 'Z' {
			out[i] = c + 'a' - 'A'
		}
	}
	return out
}

// hexWord accepts a term that is a whole hexadecimal word: no hexadecimal digit
// right before or after it.
func hexWord(before, after []byte) bool {
	return (len(before) == 0 || !isHexDigit(before[len(before)-1])) && (len(after) == 0 || !isHexDigit(after[0]))
}

func isHexDigit(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// homePath accepts a home directory: at the start of the text, after a character
// that cannot belong to a path element (a blank, a quote, "=", ":" and the like; a
// byte of a stream that is not UTF-8 included) or after "file://"; and followed by
// the end, a "/" or a character that cannot continue a name. "/var/lib/root" is not
// the home /root, "/rootfs" neither.
func homePath(before, after []byte) bool {
	if len(before) > 0 && !bytes.HasSuffix(before, []byte("file://")) && isNameByte(before[len(before)-1]) {
		return false
	}
	return len(after) == 0 || after[0] == '/' || !isNameByte(after[0])
}

// isNameByte reports an ASCII byte that goes in a path element: a letter, a digit,
// "_", ".", "-" or "/".
func isNameByte(c byte) bool {
	return c == '_' || c == '.' || c == '-' || c == '/' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// looseWord accepts a name that is a whole word: the characters around it are not
// letters, digits or underscores (a dash, a dot or a slash separate words).
func looseWord(before, after []byte) bool {
	if len(before) > 0 {
		if r, _ := utf8.DecodeLastRune(before); isWordRune(r) {
			return false
		}
	}
	if len(after) > 0 {
		if r, _ := utf8.DecodeRune(after); isWordRune(r) {
			return false
		}
	}
	return true
}
