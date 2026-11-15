//go:build !windows

package nettrace

// platformNoRouteErrnos is empty off Windows: the common errnos cover the rest.
var platformNoRouteErrnos []error
