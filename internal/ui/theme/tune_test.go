package theme

import (
	"errors"
	"image/color"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Tuning helpers for DEVDESK-770. They compute, for a colour that misses its
// contrast, the smallest move that reaches it: the same hue and HSL saturation,
// the lightness moved by steps of 0.005 away from the surfaces, until every
// surface holds. They change no colour of the palette; TestSuggestTones prints
// the proposal so it can be copied into themes/*.json and DefaultTheme.

// toneStep is the lightness step (HSL, 0 to 1) the search moves by.
const toneStep = 0.005

// errToneUnreachable says no lightness of that hue and saturation reaches the
// contrast on every surface.
var errToneUnreachable = errors.New("no lightness reaches the contrast on every surface")

// toneReq is one surface a colour has to hold against, and the ratio it needs.
type toneReq struct {
	bg  color.Color
	min float64
}

// rgb builds an opaque colour from decimal components (no hex literal).
func rgb(r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: 255} }

// toHSL returns hue in degrees [0, 360), saturation and lightness in [0, 1].
func toHSL(c color.Color) (h, s, l float64, err error) {
	v, err := rgb8(c, "tone")
	if err != nil {
		return 0, 0, 0, err
	}
	r, g, b := float64(v[0])/255, float64(v[1])/255, float64(v[2])/255
	hi, lo := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l = (hi + lo) / 2
	d := hi - lo
	if d == 0 {
		return 0, 0, l, nil
	}
	s = d / (1 - math.Abs(2*l-1))
	switch hi {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, s, l, nil
}

// fromHSL is the inverse of toHSL, rounded to 8 bits.
func fromHSL(h, s, l float64) color.RGBA {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	to8 := func(v float64) uint8 { return uint8(math.Round(math.Min(math.Max((v+m)*255, 0), 255))) }
	return rgb(to8(r), to8(g), to8(b))
}

// holdsAll reports whether c reaches every requirement.
func holdsAll(c color.Color, reqs []toneReq) (bool, error) {
	for _, r := range reqs {
		ratio, err := ContrastRatio(c, r.bg)
		if err != nil {
			return false, err
		}
		if ratio < r.min {
			return false, nil
		}
	}
	return true, nil
}

// minimalToneFor moves fg's lightness, away from the surfaces, by the smallest
// number of toneStep steps that reaches every requirement. It returns the new
// colour and its CIE76 distance to fg (0 when fg already holds). "Away" is up
// when fg is lighter than the mean luminance of the surfaces, down otherwise.
func minimalToneFor(fg color.Color, reqs []toneReq) (color.Color, float64, error) {
	if ok, err := holdsAll(fg, reqs); err != nil {
		return nil, 0, err
	} else if ok {
		return fg, 0, nil
	}
	h, s, l, err := toHSL(fg)
	if err != nil {
		return nil, 0, err
	}
	lf, err := relativeLuminance(fg)
	if err != nil {
		return nil, 0, err
	}
	var mean float64
	for _, r := range reqs {
		lb, err := relativeLuminance(r.bg)
		if err != nil {
			return nil, 0, err
		}
		mean += lb / float64(len(reqs))
	}
	dir := -1.0
	if lf >= mean {
		dir = 1
	}
	// Away from the surfaces first; the other way only when that cannot reach
	// the contrast (a mid-tone surface, or one on each side).
	for _, d := range []float64{dir, -dir} {
		if cand, ok, err := walkTone(h, s, l, d, reqs); err != nil {
			return nil, 0, err
		} else if ok {
			de, err := ColorDistance(fg, cand)
			return cand, de, err
		}
	}
	return nil, 0, errToneUnreachable
}

// walkTone moves the lightness from l by toneStep in direction dir until every
// requirement holds, and reports false when it runs out of lightness first.
func walkTone(h, s, l, dir float64, reqs []toneReq) (color.Color, bool, error) {
	for i := 1; ; i++ {
		nl := l + dir*float64(i)*toneStep
		last := nl <= 0 || nl >= 1
		nl = math.Min(math.Max(nl, 0), 1)
		cand := fromHSL(h, s, nl)
		if ok, err := holdsAll(cand, reqs); err != nil {
			return nil, false, err
		} else if ok {
			return cand, true, nil
		}
		if last {
			return nil, false, nil
		}
	}
}

// minimalTone is minimalToneFor with one ratio for every surface.
func minimalTone(fg color.Color, bgs []color.Color, min float64) (color.Color, float64, error) {
	reqs := make([]toneReq, len(bgs))
	for i, b := range bgs {
		reqs[i] = toneReq{b, min}
	}
	return minimalToneFor(fg, reqs)
}

// minimalPair reaches min between fg and bg by moving the text or the surface,
// whichever costs the smaller delta E. It returns both colours (one unchanged)
// and the distance moved.
func minimalPair(fg, bg color.Color, min float64) (color.Color, color.Color, float64, error) {
	nf, df, errF := minimalTone(fg, []color.Color{bg}, min)
	nb, db, errB := minimalTone(bg, []color.Color{fg}, min)
	switch {
	case errF != nil && errB != nil:
		return nil, nil, 0, errF
	case errB != nil || (errF == nil && df <= db):
		return nf, bg, df, nil
	default:
		return fg, nb, db, nil
	}
}

// hueGap is the circular distance in degrees between two hues.
func hueGap(a, b float64) float64 {
	d := math.Abs(a - b)
	return math.Min(d, 360-d)
}

func TestMinimalTone(t *testing.T) {
	darkBg := rgb(30, 30, 46)
	darkBg2 := rgb(49, 50, 68)
	lightBg := rgb(239, 241, 245)
	cases := []struct {
		name     string
		fg       color.Color
		bgs      []color.Color
		min      float64
		wantErr  bool
		wantSame bool
		wantDir  int // +1 lighter, -1 darker, 0 unchecked
	}{
		{"already passes", rgb(205, 214, 244), []color.Color{darkBg}, 4.5, false, true, 0},
		{"dark surface lightens", rgb(88, 91, 112), []color.Color{darkBg, darkBg2}, 4.5, false, false, +1},
		{"light surface darkens", rgb(172, 176, 190), []color.Color{lightBg}, 4.5, false, false, -1},
		{"saturated on dark", rgb(40, 60, 190), []color.Color{darkBg}, 4.5, false, false, +1},
		{"saturated on light", rgb(250, 140, 60), []color.Color{lightBg}, 4.5, false, false, -1},
		{"two surfaces at once", rgb(120, 125, 150), []color.Color{darkBg, darkBg2}, 4.5, false, false, +1},
		{"impossible on black and white", rgb(128, 128, 128), []color.Color{rgb(0, 0, 0), rgb(255, 255, 255)}, 5, true, false, 0},
		{"impossible ratio", rgb(128, 128, 128), []color.Color{darkBg}, 22, true, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, de, err := minimalTone(c.fg, c.bgs, c.min)
			if c.wantErr {
				if !errors.Is(err, errToneUnreachable) {
					t.Fatalf("err = %v, want errToneUnreachable (got %v, ΔE %.2f)", err, got, de)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.wantSame {
				if got != c.fg || de != 0 {
					t.Errorf("a colour that passes moved: %s -> %s, ΔE %.2f", Hex(c.fg), Hex(got), de)
				}
				return
			}
			for _, bg := range c.bgs {
				if r, _ := ContrastRatio(got, bg); r < c.min {
					t.Errorf("%s on %s is %.2f:1, want >= %.1f:1", Hex(got), Hex(bg), r, c.min)
				}
			}
			h0, _, l0, _ := toHSL(c.fg)
			h1, _, l1, _ := toHSL(got)
			if c.wantDir > 0 && l1 <= l0 || c.wantDir < 0 && l1 >= l0 {
				t.Errorf("lightness %.3f -> %.3f, want it to go %+d", l0, l1, c.wantDir)
			}
			if g := hueGap(h0, h1); g > 1 {
				t.Errorf("hue moved by %.2f degrees (%s -> %s), want <= 1", g, Hex(c.fg), Hex(got))
			}
			if de <= 0 {
				t.Errorf("ΔE = %.2f for a colour that moved", de)
			}
		})
	}
}

func TestMinimalToneIsMinimal(t *testing.T) {
	bg := rgb(30, 30, 46)
	fg := rgb(88, 91, 112)
	got, _, err := minimalTone(fg, []color.Color{bg}, 4.5)
	if err != nil {
		t.Fatal(err)
	}
	h, s, l, _ := toHSL(got)
	// One step less must not hold, or the move was larger than needed.
	prev := fromHSL(h, s, l-toneStep)
	if r, _ := ContrastRatio(prev, bg); r >= 4.5 {
		t.Errorf("one step earlier (%s) already holds at %.2f:1", Hex(prev), r)
	}
}

func TestMinimalPair(t *testing.T) {
	cases := []struct {
		name   string
		fg, bg color.Color
		min    float64
	}{
		{"already passes", rgb(255, 255, 255), rgb(0, 0, 0), 4.5},
		{"low pair", rgb(130, 140, 160), rgb(100, 110, 130), 4.5},
		{"same colour", rgb(120, 120, 120), rgb(120, 120, 120), 4.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nf, nb, de, err := minimalPair(c.fg, c.bg, c.min)
			if err != nil {
				t.Fatal(err)
			}
			if r, _ := ContrastRatio(nf, nb); r < c.min {
				t.Errorf("%s on %s is %.2f:1, want >= %.1f:1", Hex(nf), Hex(nb), r, c.min)
			}
			if nf != c.fg && nb != c.bg {
				t.Error("both colours moved: only one may")
			}
			_, df, errF := minimalTone(c.fg, []color.Color{c.bg}, c.min)
			_, db, errB := minimalTone(c.bg, []color.Color{c.fg}, c.min)
			best := math.Inf(1)
			if errF == nil {
				best = math.Min(best, df)
			}
			if errB == nil {
				best = math.Min(best, db)
			}
			if de > best+1e-9 {
				t.Errorf("ΔE %.2f, but moving one side costs %.2f", de, best)
			}
		})
	}
}

// themeKeyState tells where a theme file keeps a key: its value when it is
// written, "fallback" when the key is absent and the colour comes from another.
func themeKeyState(th *Theme, key string) string {
	if key == "" {
		return "no key (alias)"
	}
	v := reflect.ValueOf(th).Elem()
	for i := 0; i < v.NumField(); i++ {
		tag, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("json"), ",")
		if tag == key {
			if s := v.Field(i).String(); s != "" {
				return "explicit " + s
			}
			return "fallback (key absent: add it)"
		}
	}
	return "unknown key"
}

// TestSuggestTones prints, for every failing colour of every theme, the key to
// change, the colour before and after, and the delta E of the move. It asserts
// nothing and is skipped unless DEVDESK_THEME_TUNE=1; run it with -v:
//
//	DEVDESK_THEME_TUNE=1 go test ./internal/ui/theme/ -run TestSuggestTones -v -count=1
//
// A single colour is moved against all of its surfaces at once. Keys that other
// colours fall back to (color_secondary, color_primary) also move those: check
// the lines of the same theme together.
func TestSuggestTones(t *testing.T) {
	if os.Getenv("DEVDESK_THEME_TUNE") != "1" {
		t.Skip("set DEVDESK_THEME_TUNE=1 to print the suggested tones")
	}
	themes := shippedThemes(t)
	eachShippedTheme(t, func(t *testing.T, name string) {
		th := themes[name]
		for _, ink := range textInks() {
			fg := ink.fg()
			reqs := make([]toneReq, 0, len(ink.on))
			failing := false
			for _, need := range ink.on {
				reqs = append(reqs, toneReq{need.bg(), need.min})
				if r, err := ContrastRatio(fg, need.bg()); err != nil || r < need.min {
					failing = true
				}
			}
			if !failing {
				continue
			}
			after, de, err := minimalToneFor(fg, reqs)
			if err != nil {
				t.Logf("TUNE | %s | %s (%s) | %s | NO SOLUTION: %v", name, ink.what, ink.key, Hex(fg), err)
				continue
			}
			t.Logf("TUNE | %s | %s (%s) | %s -> %s | dE %.1f | %s", name, ink.what, ink.key, Hex(fg), Hex(after), de, themeKeyState(th, ink.key))
		}
		for _, p := range textPairs() {
			fg, bg := p.get()
			if r, err := ContrastRatio(fg, bg); err == nil && r >= p.min {
				continue
			}
			nf, nb, de, err := minimalPair(fg, bg, p.min)
			if err != nil {
				t.Logf("TUNE | %s | %s | %s on %s | NO SOLUTION: %v", name, p.what, Hex(fg), Hex(bg), err)
				continue
			}
			if nf != fg {
				t.Logf("TUNE | %s | %s (%s) | %s -> %s | dE %.1f | %s | on %s", name, p.what, p.fgKey, Hex(fg), Hex(nf), de, themeKeyState(th, p.fgKey), Hex(bg))
			} else {
				t.Logf("TUNE | %s | %s (%s) | %s -> %s | dE %.1f | %s | under %s", name, p.what, p.bgKey, Hex(bg), Hex(nb), de, themeKeyState(th, p.bgKey), Hex(fg))
			}
		}
	})
}
