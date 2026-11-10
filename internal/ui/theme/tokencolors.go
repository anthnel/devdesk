package theme

import (
	"image/color"
	"math"

	"charm.land/lipgloss/v2"
)

// TokenRole is what a segment of the token-mix bar counts. Like IconRole it is
// a meaning, so a theme can have an opinion about it and a view only names it.
type TokenRole string

const (
	TokenRoleInput      TokenRole = "input"
	TokenRoleOutput     TokenRole = "output"
	TokenRoleCacheWrite TokenRole = "cache-write"
	TokenRoleCacheRead  TokenRole = "cache-read"
)

// TokenRoles lists the roles in bar order (Input, Output, Cache write, Cache read).
func TokenRoles() []TokenRole {
	return []TokenRole{TokenRoleInput, TokenRoleOutput, TokenRoleCacheWrite, TokenRoleCacheRead}
}

// TokenColor is the colour a role is painted in.
//
// An unrecognised role falls back to the ordinary text colour rather than to
// the zero Color, for the same reason IconColor does: an empty colour inherits
// whatever the terminal last emitted (Rule 122).
func TokenColor(role TokenRole) color.Color {
	switch role {
	case TokenRoleInput:
		return ColorTokenInput
	case TokenRoleOutput:
		return ColorTokenOutput
	case TokenRoleCacheWrite:
		return ColorTokenCacheWrite
	case TokenRoleCacheRead:
		return ColorTokenCacheRead
	}
	return ColorText
}

// TokenStyle is the foreground-only style of a role; datatable and the bar
// supply the background themselves (Rule 122).
func TokenStyle(role TokenRole) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(TokenColor(role))
}

// ColorDistance is the CIE76 delta E between two colours, in Lab (D65).
// Colours are color.RGBA values; anything else is an error.
func ColorDistance(a, b color.Color) (float64, error) {
	la, err := hexToLab(a)
	if err != nil {
		return 0, err
	}
	lb, err := hexToLab(b)
	if err != nil {
		return 0, err
	}
	dl, da, db := la[0]-lb[0], la[1]-lb[1], la[2]-lb[2]
	return math.Sqrt(dl*dl + da*da + db*db), nil
}

// hexToLab converts a colour to CIE Lab with a D65 white point.
func hexToLab(c color.Color) ([3]float64, error) {
	rgb, err := rgb8(c, "color distance")
	if err != nil {
		return [3]float64{}, err
	}
	var lin [3]float64
	for i, v := range rgb {
		f := float64(v) / 255
		if f <= 0.04045 {
			lin[i] = f / 12.92
		} else {
			lin[i] = math.Pow((f+0.055)/1.055, 2.4)
		}
	}
	x := (0.4124564*lin[0] + 0.3575761*lin[1] + 0.1804375*lin[2]) / 0.95047
	y := 0.2126729*lin[0] + 0.7151522*lin[1] + 0.0721750*lin[2]
	z := (0.0193339*lin[0] + 0.1191920*lin[1] + 0.9503041*lin[2]) / 1.08883
	fx, fy, fz := labF(x), labF(y), labF(z)
	return [3]float64{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}, nil
}

func labF(t float64) float64 {
	const delta = 6.0 / 29.0
	if t > delta*delta*delta {
		return math.Cbrt(t)
	}
	return t/(3*delta*delta) + 4.0/29.0
}
