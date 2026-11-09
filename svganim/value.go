package svganim

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/go-gfx/oksvg"
)

// Value is what an animation sets a property to, in the one shape that
// interpolates every property the way SVG Animation and CSS Animations say:
// a colour, or a string with its numbers taken out. "10px" is the numbers [10]
// between "" and "px", "M0 0L10 20" is [0 0 10 20] between "M", " ", "L", " "
// and "": two values with the same skeleton (the same text around their
// numbers) interpolate number by number, which is how a list of lengths, a
// transform list and a path's data (SVG Animations Level 2, 4.1.5; CSS
// Values and Units Level 4, 'interpolation of lists') all animate; two with
// different skeletons are not interpolable and animate discretely.
type Value struct {
	Color *color.NRGBA // set: a colour (alpha is ignored by the writers)
	Skel  []string     // len(Nums)+1 pieces of text
	Nums  []float64
}

// maxValueNumbers bounds the numbers one value holds: a path of a million
// segments is not an animation.
const maxValueNumbers = 1 << 14

// ParseValue reads a value. It never fails: text with no number is a value
// without numbers.
func ParseValue(s string) Value {
	s = strings.TrimSpace(s)
	if c, ok := parseColor(s); ok {
		return Value{Color: &c}
	}
	var v Value
	var sk strings.Builder
	for i := 0; i < len(s); {
		if n, w, ok := scanNumber(s, i); ok && len(v.Nums) < maxValueNumbers {
			v.Skel = append(v.Skel, sk.String())
			sk.Reset()
			v.Nums = append(v.Nums, n)
			i += w
			continue
		}
		sk.WriteByte(s[i])
		i++
	}
	v.Skel = append(v.Skel, sk.String())
	return v
}

// scanNumber reads the number starting at s[i], when it starts one that is
// not the tail of a word (the 2 of "h2" belongs to an identifier, not a
// number; the 3 of "M3" is a number after a command).
func scanNumber(s string, i int) (n float64, width int, ok bool) {
	j := i
	if j < len(s) && (s[j] == '+' || s[j] == '-') {
		j++
	}
	d := j
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	digits := j > d
	if j < len(s) && s[j] == '.' {
		j++
		f := j
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		digits = digits || j > f
	}
	if !digits {
		return 0, 0, false
	}
	if j < len(s) && (s[j] == 'e' || s[j] == 'E') {
		k := j + 1
		if k < len(s) && (s[k] == '+' || s[k] == '-') {
			k++
		}
		if k < len(s) && s[k] >= '0' && s[k] <= '9' {
			for k < len(s) && s[k] >= '0' && s[k] <= '9' {
				k++
			}
			j = k
		}
	}
	// the tail of an identifier ("h2", "scale3d") is not a number
	if i > 0 && isIdent(s[i-1]) && !isCommand(s[i-1]) {
		return 0, 0, false
	}
	f, err := strconv.ParseFloat(s[i:j], 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, 0, false
	}
	return f, j - i, true
}

func isIdent(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

// isCommand reports a path command letter, after which a number is a number.
func isCommand(c byte) bool { return strings.IndexByte("MmLlHhVvCcSsQqTtAaZz", c) >= 0 }

func parseColor(s string) (c color.NRGBA, ok bool) {
	if s == "" || s == "none" || s == "currentColor" || strings.HasPrefix(s, "url") {
		return c, false
	}
	if s[0] >= '0' && s[0] <= '9' || s[0] == '-' || s[0] == '.' || s[0] == '+' {
		return c, false
	}
	defer func() {
		if recover() != nil {
			c, ok = color.NRGBA{}, false
		}
	}()
	col, err := oksvg.ParseSVGColor(s)
	if err != nil || col == nil {
		return c, false
	}
	n, isN := col.(color.NRGBA)
	if !isN {
		return c, false
	}
	// The default branch of ParseSVGColor answers black for a word it does
	// not know ("round", "linear"): only a real colour is one.
	if n == (color.NRGBA{0, 0, 0, 255}) && !isBlack(s) {
		return c, false
	}
	return n, true
}

func isBlack(s string) bool {
	switch strings.ToLower(s) {
	case "black", "#000", "#000000", "rgb(0,0,0)", "rgb(0, 0, 0)":
		return true
	}
	return false
}

// String is the value as SVG or CSS text.
func (v Value) String() string {
	if v.Color != nil {
		return fmt.Sprintf("#%02x%02x%02x", v.Color.R, v.Color.G, v.Color.B)
	}
	var b strings.Builder
	for i, sk := range v.Skel {
		b.WriteString(sk)
		if i < len(v.Nums) {
			b.WriteString(strconv.FormatFloat(v.Nums[i], 'g', 8, 64))
		}
	}
	return b.String()
}

// Interpolable reports that v and w have the same shape: both colours, or
// the same text around the same count of numbers.
func (v Value) Interpolable(w Value) bool {
	if (v.Color != nil) != (w.Color != nil) {
		return false
	}
	if v.Color != nil {
		return true
	}
	if len(v.Nums) != len(w.Nums) || len(v.Skel) != len(w.Skel) {
		return false
	}
	for i := range v.Skel {
		if normSkel(v.Skel[i]) != normSkel(w.Skel[i]) {
			return false
		}
	}
	return true
}

// normSkel makes "," and " " the same separator, as a list's two spellings.
func normSkel(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' }), " ")
}

// Lerp is the value p of the way from v to w (p may leave [0,1], as a spline
// that overshoots does). It reports false when they are not Interpolable.
func (v Value) Lerp(w Value, p float64) (Value, bool) {
	if !v.Interpolable(w) {
		return v, false
	}
	if v.Color != nil {
		mix := func(a, b uint8) uint8 {
			return uint8(math.Round(math.Max(0, math.Min(255, float64(a)+(float64(b)-float64(a))*p))))
		}
		c := color.NRGBA{mix(v.Color.R, w.Color.R), mix(v.Color.G, w.Color.G), mix(v.Color.B, w.Color.B), mix(v.Color.A, w.Color.A)}
		return Value{Color: &c}, true
	}
	out := Value{Skel: v.Skel, Nums: make([]float64, len(v.Nums))}
	for i := range v.Nums {
		out.Nums[i] = v.Nums[i] + (w.Nums[i]-v.Nums[i])*p
	}
	return out, true
}

// Add is v plus w, number by number (additive animation); a colour adds per
// channel, clamped.
func (v Value) Add(w Value) (Value, bool) {
	if !v.Interpolable(w) {
		return w, false
	}
	if v.Color != nil {
		add := func(a, b uint8) uint8 { return uint8(math.Min(255, float64(a)+float64(b))) }
		c := color.NRGBA{add(v.Color.R, w.Color.R), add(v.Color.G, w.Color.G), add(v.Color.B, w.Color.B), 255}
		return Value{Color: &c}, true
	}
	out := Value{Skel: v.Skel, Nums: make([]float64, len(v.Nums))}
	for i := range v.Nums {
		out.Nums[i] = v.Nums[i] + w.Nums[i]
	}
	return out, true
}

// Dist is the distance between two Interpolable values, for paced animation.
func (v Value) Dist(w Value) float64 {
	if !v.Interpolable(w) {
		return 0
	}
	var s float64
	if v.Color != nil {
		d := func(a, b uint8) float64 { return float64(a) - float64(b) }
		return math.Sqrt(d(v.Color.R, w.Color.R)*d(v.Color.R, w.Color.R) + d(v.Color.G, w.Color.G)*d(v.Color.G, w.Color.G) + d(v.Color.B, w.Color.B)*d(v.Color.B, w.Color.B))
	}
	for i := range v.Nums {
		s += (v.Nums[i] - w.Nums[i]) * (v.Nums[i] - w.Nums[i])
	}
	return math.Sqrt(s)
}
