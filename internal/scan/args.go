package scan

import (
	"errors"
	"strings"
)

// A tool's extra arguments are stored as a YAML list and edited as one line of
// text. These two convert between the forms: spaces separate, and single or
// double quotes keep a value that holds some together — the shell's rule, which
// is what anyone typing a command line expects. There is no escaping beyond
// that, and no expansion: the arguments are passed as they are, never to a shell.

// SplitArgs cuts one line into arguments.
func SplitArgs(line string) ([]string, error) {
	var (
		args    []string
		current strings.Builder
		quote   rune
		inArg   bool
	)
	for _, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			current.WriteRune(r)
		case r == '"' || r == '\'':
			quote, inArg = r, true
		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, current.String())
				current.Reset()
				inArg = false
			}
		default:
			current.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, errors.New("a quote is not closed")
	}
	if inArg {
		args = append(args, current.String())
	}
	return args, nil
}

// JoinArgs writes arguments back as the line SplitArgs reads: an argument that
// is empty, or holds a space or a quote, is quoted with the quote it does not
// contain. One holding both kinds cannot be written without escaping, which
// this line does not have; it is double-quoted, and SplitArgs will read it
// differently — a limit of the one-line form, not of the list in the file.
func JoinArgs(args []string) string {
	out := make([]string, len(args))
	for i, arg := range args {
		switch {
		case arg != "" && !strings.ContainsAny(arg, " \t'\""):
			out[i] = arg
		case strings.Contains(arg, `"`) && !strings.Contains(arg, "'"):
			out[i] = "'" + arg + "'"
		default:
			out[i] = `"` + arg + `"`
		}
	}
	return strings.Join(out, " ")
}
