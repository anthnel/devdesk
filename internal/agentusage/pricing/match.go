package pricing

// dateSuffixLen is the length of a trailing "-YYYYMMDD".
const dateSuffixLen = 9

// Lookup returns the rates of a model. It matches the normalized name
// exactly, then once more without one trailing "-YYYYMMDD". There is no
// prefix matching, and nothing else is stripped ("[1m]", provider
// spellings): a key such as claude-sonnet-5 must never price
// claude-sonnet-5-5.
func (t Table) Lookup(model string) (Rates, bool) {
	name := normalize(model)
	if name == "" {
		return Rates{}, false
	}
	if r, ok := t.rates[name]; ok {
		return r, true
	}
	if base, ok := withoutDate(name); ok {
		if r, ok := t.rates[base]; ok {
			return r, true
		}
	}
	return Rates{}, false
}

// withoutDate removes one trailing "-" plus eight digits.
func withoutDate(name string) (string, bool) {
	n := len(name)
	if n <= dateSuffixLen || name[n-dateSuffixLen] != '-' {
		return "", false
	}
	for i := n - dateSuffixLen + 1; i < n; i++ {
		if name[i] < '0' || name[i] > '9' {
			return "", false
		}
	}
	return name[:n-dateSuffixLen], true
}
