package jobs

import (
	"regexp"
)

var (
	// userinfo between "scheme://" and the host: everything up to the LAST "@"
	// before the first "/", so a raw "@" in a password does not leak its tail.
	urlUserinfo = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^\s/'"]*@`)
	// the value of a secret-bearing query or fragment parameter, anchored on
	// the "?", "&" or "#" that precedes its name; the value stops at "&", a
	// space, a quote or "#".
	secretParam = regexp.MustCompile(`(?i)([?&#][a-z_-]*(?:token|password|passwd|secret|key)=)[^&\s'"#]*`)
	// the credential of an Authorization or PRIVATE-TOKEN header.
	authHeader = regexp.MustCompile(`(?i)\b(authorization:\s*(?:bearer|basic|token)\s+|private-token:\s*)[^\s'"]+`)
)

// SafeCause renders an error for display and logs with the credentials a URL
// in its text may carry removed: the userinfo of any scheme://, the values of
// secret query parameters (token, password, secret, key families) and the
// credential of Authorization and PRIVATE-TOKEN headers. The scp form (git@host:path) carries no
// secret and is left alone.
func SafeCause(err error) string {
	if err == nil {
		return ""
	}
	s := urlUserinfo.ReplaceAllString(err.Error(), "$1")
	s = secretParam.ReplaceAllString(s, "${1}")
	return authHeader.ReplaceAllString(s, "${1}")
}
