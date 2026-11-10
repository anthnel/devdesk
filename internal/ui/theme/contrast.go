package theme

import (
	"fmt"
	"image/color"
	"math"
)

// ContrastRatio returns the WCAG 2.x contrast ratio (1 to 21) between two
// colors that are a color.RGBA (what Hex-written #rgb and #rrggbb parse to).
// Any other colour is an error. The ratio is symmetric: (L1+0.05)/(L2+0.05) with L1 the lighter.
func ContrastRatio(fg, bg color.Color) (float64, error) {
	lf, err := relativeLuminance(fg)
	if err != nil {
		return 0, err
	}
	lb, err := relativeLuminance(bg)
	if err != nil {
		return 0, err
	}
	if lf < lb {
		lf, lb = lb, lf
	}
	return (lf + 0.05) / (lb + 0.05), nil
}

// rgb8 returns the 8-bit components of c. Only a color.RGBA is a real colour:
// NoColor.RGBA() is opaque black and an ANSI index has no fixed value, so
// reading either would pass a non-colour off as one. who prefixes the error.
func rgb8(c color.Color, who string) ([3]uint8, error) {
	rgba, ok := c.(color.RGBA)
	if !ok {
		return [3]uint8{}, fmt.Errorf("%s: %s is not a #rgb or #rrggbb color", who, describe(c))
	}
	r, g, b, _ := rgba.RGBA()
	return [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}, nil
}

// describe names a colour that is not a color.RGBA for an error message.
func describe(c color.Color) string {
	if c == nil {
		return "nil"
	}
	return fmt.Sprintf("%T(%v)", c, c)
}

// relativeLuminance computes the WCAG relative luminance of an sRGB color.
func relativeLuminance(c color.Color) (float64, error) {
	rgb, err := rgb8(c, "contrast")
	if err != nil {
		return 0, err
	}
	var ch [3]float64
	for i, v := range rgb {
		f := float64(v) / 255
		if f <= 0.03928 {
			ch[i] = f / 12.92
		} else {
			ch[i] = math.Pow((f+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*ch[0] + 0.7152*ch[1] + 0.0722*ch[2], nil
}
