package theme

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ContrastRatio returns the WCAG 2.x contrast ratio (1 to 21) between two
// colors written as #rgb or #rrggbb (case-insensitive). Any other form is an
// error. The ratio is symmetric: (L1+0.05)/(L2+0.05) with L1 the lighter.
func ContrastRatio(fg, bg lipgloss.Color) (float64, error) {
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

// relativeLuminance computes the WCAG relative luminance of an sRGB color.
func relativeLuminance(c lipgloss.Color) (float64, error) {
	s := string(c)
	if !strings.HasPrefix(s, "#") {
		return 0, fmt.Errorf("contrast: %q is not a #rgb or #rrggbb color", s)
	}
	h := s[1:]
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		return 0, fmt.Errorf("contrast: %q is not a #rgb or #rrggbb color", s)
	}
	var ch [3]float64
	for i := range ch {
		v, err := strconv.ParseUint(h[i*2:i*2+2], 16, 8)
		if err != nil {
			return 0, fmt.Errorf("contrast: %q is not a #rgb or #rrggbb color", s)
		}
		f := float64(v) / 255
		if f <= 0.03928 {
			ch[i] = f / 12.92
		} else {
			ch[i] = math.Pow((f+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*ch[0] + 0.7152*ch[1] + 0.0722*ch[2], nil
}
