package shortcut

// HeaderRows is the header's fixed height, without the blank line under it (Rule 124).
const HeaderRows = 7

// HeaderInfoSeparator sits between a header key (padded to the longest) and its value.
const HeaderInfoSeparator = ": "

// HeaderInfoWidth is the width of the header's info column for a terminal termWidth
// cells wide: a quarter of what the two padding cells leave; 0 below 2 cells.
func HeaderInfoWidth(termWidth int) int {
	if termWidth < 2 {
		return 0
	}
	return (termWidth - 2) / 4
}

// HeaderValueWidth is the room a value has on an info line whose keys are keys,
// aligned on the longest one (byte length, as the router's info lines do):
// HeaderInfoWidth minus that key minus the separator, floored at 0.
func HeaderValueWidth(termWidth int, keys []string) int {
	longest := 0
	for _, k := range keys {
		longest = max(longest, len(k))
	}
	return max(HeaderInfoWidth(termWidth)-longest-len(HeaderInfoSeparator), 0)
}
