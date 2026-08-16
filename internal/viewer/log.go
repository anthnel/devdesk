package viewer

import (
	"regexp"
	"strings"
)

// Level is a log line's severity, ordered so a filter can be a single
// comparison.
//
// LevelUnknown is zero and is *not* the bottom of the scale — it means nobody
// could tell. Passes treats it separately for that reason.
type Level int

const (
	LevelUnknown Level = iota
	LevelTrace
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
)

// Levels are the filter's positions, ascending. LevelUnknown is absent: it is
// not a level a user can filter to, it is the absence of one.
var Levels = []Level{LevelTrace, LevelDebug, LevelInfo, LevelWarn, LevelError}

func (l Level) String() string {
	switch l {
	case LevelTrace:
		return "trace"
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "unknown"
	}
}

// Passes reports whether a line survives a minimum-level filter.
//
// An unlevelled line always passes, and that is the whole point of the filter
// being usable: a stack trace is a dozen lines with no level on any of them,
// and a filter that swallowed them would destroy exactly what the user opened
// the log to read. Inheritance (see ParseLog) covers the lines that belong to
// an entry above them; this covers the ones that belong to nothing.
func (l Level) Passes(min Level) bool {
	return l == LevelUnknown || min == LevelUnknown || l >= min
}

// LogLine is one line and what could be worked out about it.
type LogLine struct {
	Text  string
	Level Level
}

// ParseLog splits text into levelled lines.
//
// A line with no level of its own inherits the effective level of the line
// above it. That is what makes a stack trace survive a filter set to its
// parent's level, and it chains: twenty continuation lines all carry the level
// of the entry that started them.
//
// The trade is real and stated here rather than discovered later: a genuinely
// unrelated line with no level, following an INFO, is filtered out with it.
// Losing a stray line beats losing a stack trace, and it is where lnav and
// stern landed too.
func ParseLog(text string) []LogLine {
	raw := strings.Split(text, "\n")
	lines := make([]LogLine, 0, len(raw))

	inherited := LevelUnknown
	for _, line := range raw {
		level := detectLevel(line)
		if level == LevelUnknown {
			// Blank lines break the chain: they are the one thing that reliably
			// separates one entry from the next, and letting a level carry
			// across them would colour half a file from a single ERROR.
			if strings.TrimSpace(line) == "" {
				inherited = LevelUnknown
			}
			level = inherited
		} else {
			inherited = level
		}
		lines = append(lines, LogLine{Text: line, Level: level})
	}
	return lines
}

// Three shapes, because a container's log is as likely to be any of them. They
// are tried in order of how certain they are: a structured field means what it
// says, a bare word in a line is a guess.
var (
	jsonLevelRe = regexp.MustCompile(`"(?:level|severity|lvl)"\s*:\s*"([A-Za-z]+)"`)
	// The optional quote is not paired with a closing one: RE2 has no
	// backreferences, and `level="warn` is not a shape worth a second pattern.
	logfmtLevelRe = regexp.MustCompile(`(?i)\b(?:level|lvl|severity)="?([A-Za-z]+)`)

	// Uppercase only, and only as a standalone token. A lowercase "error" is a
	// word people write in sentences; an uppercase one, delimited, is a field.
	// Refusing lowercase here costs a few applications their colour and saves
	// every line that merely mentions the word from being filtered as one.
	// The trailing class includes "[" for logrus, whose shape is `ERRO[0001]`:
	// the level runs straight into the bracket that follows it.
	bareLevelRe = regexp.MustCompile(`(?:^|[\s\[(|])(FATAL|PANIC|ERROR|ERRO|ERR|WARNING|WARN|WRN|NOTICE|INFO|INF|DEBUG|DEBU|DBG|TRACE|TRC)(?:[\s\[\]()|:=]|$)`)
)

// bareLevelWindow is how far into a line a bare level token is still credible.
// Past a timestamp, a hostname and a logger name, a word in the message body is
// prose.
const bareLevelWindow = 64

func detectLevel(line string) Level {
	if m := jsonLevelRe.FindStringSubmatch(line); m != nil {
		if level, ok := ParseLevel(m[1]); ok {
			return level
		}
	}
	if m := logfmtLevelRe.FindStringSubmatch(line); m != nil {
		if level, ok := ParseLevel(m[1]); ok {
			return level
		}
	}

	head := line
	if len(head) > bareLevelWindow {
		head = head[:bareLevelWindow]
	}
	if m := bareLevelRe.FindStringSubmatch(head); m != nil {
		if level, ok := ParseLevel(m[1]); ok {
			return level
		}
	}
	return LevelUnknown
}

// levelWords maps every spelling to its level. Fatal and panic fold into error:
// the filter asks "how bad", and a process dying is not less bad than an error.
var levelWords = map[string]Level{
	"fatal":   LevelError,
	"panic":   LevelError,
	"error":   LevelError,
	"erro":    LevelError,
	"err":     LevelError,
	"warning": LevelWarn,
	"warn":    LevelWarn,
	"wrn":     LevelWarn,
	"notice":  LevelInfo,
	"info":    LevelInfo,
	"inf":     LevelInfo,
	"debug":   LevelDebug,
	"debu":    LevelDebug,
	"dbg":     LevelDebug,
	"trace":   LevelTrace,
	"trc":     LevelTrace,
}

// ParseLevel resolves a spelling to a level. The bool separates "not a level"
// from "the zero level", which a bare Level return could not.
func ParseLevel(word string) (Level, bool) {
	level, ok := levelWords[strings.ToLower(word)]
	return level, ok
}
