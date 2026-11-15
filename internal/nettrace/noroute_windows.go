//go:build windows

package nettrace

import "golang.org/x/sys/windows"

// platformNoRouteErrnos are the raw Windows codes for "no route": the Winsock
// ones and the IP Helper ones go-netroute can return.
var platformNoRouteErrnos = []error{
	windows.WSAENETUNREACH,
	windows.WSAEHOSTUNREACH,
	windows.ERROR_NETWORK_UNREACHABLE,
	windows.ERROR_HOST_UNREACHABLE,
}
