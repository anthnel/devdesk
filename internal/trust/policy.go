package trust

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// PolicyVersion is the only `version:` a policy file may declare.
const PolicyVersion = 1

// Policy is C: the rules of ~/.devdesk/trust.yaml, in file order.
type Policy struct {
	Rules []Rule
}

// fileRule is one rule as it is written. Exactly one of Key, Keyless, Expect
// and Notation may be set: the mode names the tool, so there is no `tool:`
// field that could contradict it.
type fileRule struct {
	Match    string         `yaml:"match"`
	Key      string         `yaml:"key"`
	TLog     *bool          `yaml:"tlog"`
	Keyless  *fileKeyless   `yaml:"keyless"`
	Expect   string         `yaml:"expect"`
	Notation map[string]any `yaml:"notation"`
}

type fileKeyless struct {
	Issuer        string `yaml:"issuer"`
	Subject       string `yaml:"subject"`
	SubjectRegexp string `yaml:"subject_regexp"`
}

type policyFile struct {
	Version int        `yaml:"version"`
	Rules   []fileRule `yaml:"rules"`
}

// PolicyPath is where the user's rules live. It is global, not a context's:
// which identity to trust is a fact about the world, not a setting of one
// target (§3.82) — and ~/.devdesk/config.yaml is the default context, not a
// global file.
func PolicyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".devdesk", "trust.yaml"), nil
}

// LoadDefault reads the policy at PolicyPath.
func LoadDefault() (Policy, error) {
	path, err := PolicyPath()
	if err != nil {
		return Policy{}, err
	}
	p, _, err := Load(path)
	return p, err
}

// Load reads a policy file. A file that does not exist is an empty policy.
//
// It is strict where config.Load is not: an unknown key is an error, and an
// error rejects the whole file. A typo such as `isuer:` would otherwise drop
// the issuer from a rule without a word, and a rule weakened in silence is
// worse than no rule. The warnings are rules no image can reach.
func Load(path string) (Policy, []string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Policy{}, nil, nil
	}
	if err != nil {
		return Policy{}, nil, err
	}
	return parsePolicy(path, data)
}

func parsePolicy(path string, data []byte) (Policy, []string, error) {
	var file policyFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil {
		// An empty file decodes to io.EOF: it declares no version.
		if errors.Is(err, io.EOF) {
			return Policy{}, nil, fmt.Errorf("%s: version must be %d", path, PolicyVersion)
		}
		return Policy{}, nil, fmt.Errorf("%s: %w", path, err)
	}
	if file.Version != PolicyVersion {
		return Policy{}, nil, fmt.Errorf("%s: version must be %d, got %d", path, PolicyVersion, file.Version)
	}

	lines := ruleLines(data)
	dir := filepath.Dir(path)
	var policy Policy
	var warnings []string
	for i, fr := range file.Rules {
		origin := fmt.Sprintf("%s:%d", path, lineOf(lines, i))
		rule, err := fr.rule(origin, dir)
		if err != nil {
			return Policy{}, nil, fmt.Errorf("%s: %w", origin, err)
		}
		for _, prev := range policy.Rules {
			if shadows(prev.Match, rule.Match) {
				warnings = append(warnings, fmt.Sprintf("%s: never used, %s already matches %s", origin, prev.Origin, rule.Match))
				break
			}
		}
		policy.Rules = append(policy.Rules, rule)
	}
	return policy, warnings, nil
}

// ruleLines is the line of each rule, for the origin a refusal names. The
// strict decode above cannot give it; a second, lenient pass over the node tree
// can.
func ruleLines(data []byte) []int {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "rules" {
			continue
		}
		var out []int
		for _, item := range root.Content[i+1].Content {
			out = append(out, item.Line)
		}
		return out
	}
	return nil
}

func lineOf(lines []int, i int) int {
	if i < len(lines) {
		return lines[i]
	}
	return 0
}

func (fr fileRule) rule(origin, dir string) (Rule, error) {
	if fr.Match == "" {
		return Rule{}, errors.New("match is required")
	}
	if !validPattern(fr.Match) {
		return Rule{}, fmt.Errorf("match %q is not a valid pattern (`*` is one path segment, `/**` only at the end)", fr.Match)
	}
	modes := 0
	for _, set := range []bool{fr.Key != "", fr.Keyless != nil, fr.Expect != "", fr.Notation != nil} {
		if set {
			modes++
		}
	}
	if modes != 1 {
		return Rule{}, errors.New("a rule declares exactly one of key, keyless, expect")
	}
	if fr.TLog != nil && fr.Key == "" {
		return Rule{}, errors.New("tlog applies to key rules only")
	}

	rule := Rule{Match: fr.Match, Source: SourceUser, Origin: origin}
	switch {
	case fr.Notation != nil:
		return Rule{}, errors.New("notation is not supported yet")
	case fr.Expect != "":
		if fr.Expect != "none" {
			return Rule{}, fmt.Errorf("expect must be none, got %q", fr.Expect)
		}
		rule.Mode = ModeNone
	case fr.Keyless != nil:
		k := *fr.Keyless
		if k.Issuer == "" {
			return Rule{}, errors.New("keyless needs an issuer")
		}
		if (k.Subject == "") == (k.SubjectRegexp == "") {
			return Rule{}, errors.New("keyless needs exactly one of subject, subject_regexp")
		}
		if k.SubjectRegexp != "" {
			if _, err := regexp.Compile(k.SubjectRegexp); err != nil {
				return Rule{}, fmt.Errorf("subject_regexp: %w", err)
			}
		}
		rule.Mode, rule.Issuer, rule.Subject, rule.SubjectRegexp = ModeKeyless, k.Issuer, k.Subject, k.SubjectRegexp
	default:
		key, err := readKey(fr.Key, dir)
		if err != nil {
			return Rule{}, err
		}
		rule.Mode, rule.Keys, rule.TLog = ModeKey, []Key{key}, fr.TLog == nil || *fr.TLog
	}
	return rule, nil
}

// readKey loads a key file when the policy is read, not when an image is
// verified: an unreadable key rejects the file at once, the way a typo does,
// and the key's content — not its path — is what the cache fingerprint covers.
// A KMS URI is left for cosign to resolve.
func readKey(ref, dir string) (Key, error) {
	if strings.Contains(ref, "://") {
		return Key{Ref: ref}, nil
	}
	path := ref
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return Key{}, err
		}
		path = filepath.Join(home, rest)
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Key{}, fmt.Errorf("key: %w", err)
	}
	if !bytes.Contains(data, []byte("PUBLIC KEY-----")) {
		return Key{}, fmt.Errorf("key %s is not a PEM public key", ref)
	}
	return Key{Ref: ref, PEM: data}, nil
}
