package svganim

import (
	"math"
	"strings"
)

// Matrix is an affine transform [a b c d e f]: x' = a·x + c·y + e,
// y' = b·x + d·y + f (SVG 1.1, 7.4).
type Matrix [6]float64

// Identity is the transform that moves nothing.
var Identity = Matrix{1, 0, 0, 1, 0, 0}

// Mul is m followed by n in a transform list's reading: the point is first
// moved by n, then by m (m·n).
func (m Matrix) Mul(n Matrix) Matrix {
	return Matrix{
		m[0]*n[0] + m[2]*n[1], m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3], m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4], m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

// Apply is the image of the point.
func (m Matrix) Apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

// Inverse is the transform that undoes m; ok is false for a singular one.
func (m Matrix) Inverse() (Matrix, bool) {
	det := m[0]*m[3] - m[1]*m[2]
	if math.Abs(det) < 1e-12 || math.IsNaN(det) {
		return Identity, false
	}
	return Matrix{
		m[3] / det, -m[1] / det, -m[2] / det, m[0] / det,
		(m[2]*m[5] - m[3]*m[4]) / det, (m[1]*m[4] - m[0]*m[5]) / det,
	}, true
}

// Translate, Scale and Rotate (degrees, about the origin) are the basic
// transforms.
func Translate(x, y float64) Matrix { return Matrix{1, 0, 0, 1, x, y} }
func Scale(x, y float64) Matrix     { return Matrix{x, 0, 0, y, 0, 0} }
func Rotate(deg float64) Matrix {
	s, c := math.Sincos(deg * math.Pi / 180)
	return Matrix{c, s, -s, c, 0, 0}
}

// About is m taken about the point (cx, cy): the point stays where it is.
func (m Matrix) About(cx, cy float64) Matrix {
	return Translate(cx, cy).Mul(m).Mul(Translate(-cx, -cy))
}

// IsIdentity reports a transform within eps of the identity.
func (m Matrix) IsIdentity(eps float64) bool {
	for i, v := range m {
		if math.Abs(v-Identity[i]) > eps {
			return false
		}
	}
	return true
}

// String is the transform as an SVG attribute value.
func (m Matrix) String() string {
	return "matrix(" + Value{Skel: []string{"", " ", " ", " ", " ", " ", ""}, Nums: m[:]}.String() + ")"
}

// ParseTransform reads a transform list (SVG 1.1 7.6; CSS Transforms 1: the
// 2D functions, with deg, rad, grad and turn angles and px or unitless
// lengths). ok is false for anything else (a percentage, 3D, a var()).
func ParseTransform(s string) (Matrix, bool) {
	m := Identity
	s = strings.TrimSpace(s)
	if s == "" || s == "none" {
		return m, true
	}
	n := 0
	for s != "" {
		n++
		if n > 64 {
			return Identity, false
		}
		open := strings.IndexByte(s, '(')
		cl := strings.IndexByte(s, ')')
		if open <= 0 || cl < open {
			return Identity, false
		}
		name := strings.TrimSpace(strings.Trim(s[:open], ", \t\r\n"))
		args := strings.FieldsFunc(s[open+1:cl], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\r' || r == '\n' })
		s = strings.TrimSpace(strings.TrimLeft(s[cl+1:], ", \t\r\n"))
		var a []float64
		isAngle := strings.HasPrefix(strings.ToLower(name), "rotate") || strings.HasPrefix(strings.ToLower(name), "skew")
		for i, t := range args {
			f, ok := cssNumber(t, isAngle && !(strings.EqualFold(name, "rotate") && i > 0))
			if !ok {
				return Identity, false
			}
			a = append(a, f)
		}
		var t Matrix
		switch strings.ToLower(name) {
		case "matrix":
			if len(a) != 6 {
				return Identity, false
			}
			copy(t[:], a)
		case "translate":
			switch len(a) {
			case 1:
				t = Translate(a[0], 0)
			case 2:
				t = Translate(a[0], a[1])
			default:
				return Identity, false
			}
		case "translatex":
			if len(a) != 1 {
				return Identity, false
			}
			t = Translate(a[0], 0)
		case "translatey":
			if len(a) != 1 {
				return Identity, false
			}
			t = Translate(0, a[0])
		case "scale":
			switch len(a) {
			case 1:
				t = Scale(a[0], a[0])
			case 2:
				t = Scale(a[0], a[1])
			default:
				return Identity, false
			}
		case "scalex":
			if len(a) != 1 {
				return Identity, false
			}
			t = Scale(a[0], 1)
		case "scaley":
			if len(a) != 1 {
				return Identity, false
			}
			t = Scale(1, a[0])
		case "rotate":
			switch len(a) {
			case 1:
				t = Rotate(a[0])
			case 3:
				t = Rotate(a[0]).About(a[1], a[2])
			default:
				return Identity, false
			}
		case "skewx":
			if len(a) != 1 {
				return Identity, false
			}
			t = Matrix{1, 0, math.Tan(a[0] * math.Pi / 180), 1, 0, 0}
		case "skewy":
			if len(a) != 1 {
				return Identity, false
			}
			t = Matrix{1, math.Tan(a[0] * math.Pi / 180), 0, 1, 0, 0}
		default:
			return Identity, false
		}
		m = m.Mul(t)
	}
	return m, true
}

// cssNumber reads a number with an optional unit: degrees for an angle, px
// or none for a length.
func cssNumber(t string, angle bool) (float64, bool) {
	n, w, ok := scanNumber(t, 0)
	if !ok {
		return 0, false
	}
	unit := strings.ToLower(t[w:])
	switch unit {
	case "":
		return n, true
	case "px":
		return n, !angle
	case "deg":
		return n, angle
	case "rad":
		return n * 180 / math.Pi, angle
	case "grad":
		return n * 0.9, angle
	case "turn":
		return n * 360, angle
	}
	return 0, false
}

// Point is a position in user units.
type Point struct{ X, Y float64 }

// Seg is one step of a path, in absolute coordinates: 'M' and 'L' to P[0],
// 'C' through P[0] and P[1] to P[2], 'Z' closing.
type Seg struct {
	Op byte
	P  [3]Point
}

// maxPathSegs bounds a path read.
const maxPathSegs = 1 << 15

// ParsePath reads SVG path data (SVG 1.1, 8.3) as M, L, C and Z steps: H, V
// become L, Q and T, S and the arcs become cubic curves. ok is false for
// data that is not a path.
func ParsePath(d string) (segs []Seg, ok bool) {
	defer func() {
		if recover() != nil {
			segs, ok = nil, false
		}
	}()
	var cur, start, lastC, lastQ Point
	var prev byte
	i := 0
	skip := func() {
		for i < len(d) && (d[i] == ' ' || d[i] == ',' || d[i] == '\t' || d[i] == '\n' || d[i] == '\r') {
			i++
		}
	}
	num := func() (float64, bool) {
		skip()
		n, w, ok := scanNumberRaw(d, i)
		if !ok {
			return 0, false
		}
		i += w
		return n, true
	}
	flag := func() (float64, bool) {
		skip()
		if i < len(d) && (d[i] == '0' || d[i] == '1') {
			f := float64(d[i] - '0')
			i++
			return f, true
		}
		return 0, false
	}
	var cmd byte
	for {
		skip()
		if i >= len(d) {
			break
		}
		if isCommand(d[i]) {
			cmd = d[i]
			i++
		} else if cmd == 0 || cmd == 'Z' || cmd == 'z' {
			return nil, false
		} else if cmd == 'M' {
			cmd = 'L'
		} else if cmd == 'm' {
			cmd = 'l'
		}
		if len(segs) > maxPathSegs {
			return nil, false
		}
		rel := cmd >= 'a'
		up := cmd &^ 0x20
		var a [7]float64
		need := map[byte]int{'M': 2, 'L': 2, 'H': 1, 'V': 1, 'C': 6, 'S': 4, 'Q': 4, 'T': 2, 'A': 7, 'Z': 0}[up]
		for k := 0; k < need; k++ {
			var v float64
			var okv bool
			if up == 'A' && (k == 3 || k == 4) {
				v, okv = flag()
			} else {
				v, okv = num()
			}
			if !okv {
				return nil, false
			}
			a[k] = v
		}
		off := Point{}
		if rel {
			off = cur
		}
		switch up {
		case 'M':
			cur = Point{a[0] + off.X, a[1] + off.Y}
			start = cur
			segs = append(segs, Seg{Op: 'M', P: [3]Point{cur}})
		case 'L':
			cur = Point{a[0] + off.X, a[1] + off.Y}
			segs = append(segs, Seg{Op: 'L', P: [3]Point{cur}})
		case 'H':
			if rel {
				a[0] += cur.X
			}
			cur = Point{a[0], cur.Y}
			segs = append(segs, Seg{Op: 'L', P: [3]Point{cur}})
		case 'V':
			if rel {
				a[0] += cur.Y
			}
			cur = Point{cur.X, a[0]}
			segs = append(segs, Seg{Op: 'L', P: [3]Point{cur}})
		case 'C':
			c1, c2, e := Point{a[0] + off.X, a[1] + off.Y}, Point{a[2] + off.X, a[3] + off.Y}, Point{a[4] + off.X, a[5] + off.Y}
			segs = append(segs, Seg{Op: 'C', P: [3]Point{c1, c2, e}})
			lastC, cur = c2, e
		case 'S':
			c1 := cur
			if prev == 'C' || prev == 'S' {
				c1 = Point{2*cur.X - lastC.X, 2*cur.Y - lastC.Y}
			}
			c2, e := Point{a[0] + off.X, a[1] + off.Y}, Point{a[2] + off.X, a[3] + off.Y}
			segs = append(segs, Seg{Op: 'C', P: [3]Point{c1, c2, e}})
			lastC, cur = c2, e
		case 'Q', 'T':
			var q Point
			var e Point
			if up == 'Q' {
				q, e = Point{a[0] + off.X, a[1] + off.Y}, Point{a[2] + off.X, a[3] + off.Y}
			} else {
				q = cur
				if prev == 'Q' || prev == 'T' {
					q = Point{2*cur.X - lastQ.X, 2*cur.Y - lastQ.Y}
				}
				e = Point{a[0] + off.X, a[1] + off.Y}
			}
			segs = append(segs, Seg{Op: 'C', P: [3]Point{
				{cur.X + 2.0/3*(q.X-cur.X), cur.Y + 2.0/3*(q.Y-cur.Y)},
				{e.X + 2.0/3*(q.X-e.X), e.Y + 2.0/3*(q.Y-e.Y)}, e}})
			lastQ, cur = q, e
		case 'A':
			e := Point{a[5] + off.X, a[6] + off.Y}
			segs = append(segs, arcSegs(cur, a[0], a[1], a[2], a[3] != 0, a[4] != 0, e)...)
			cur = e
		case 'Z':
			segs = append(segs, Seg{Op: 'Z'})
			cur = start
		}
		prev = up
	}
	if len(segs) == 0 || segs[0].Op != 'M' {
		return nil, false
	}
	return segs, true
}

// scanNumberRaw reads a number at d[i] without the word test of scanNumber:
// path data runs numbers together ("10-5", ".5.5").
func scanNumberRaw(d string, i int) (float64, int, bool) {
	j := i
	if j < len(d) && (d[j] == '+' || d[j] == '-') {
		j++
	}
	k := j
	for j < len(d) && d[j] >= '0' && d[j] <= '9' {
		j++
	}
	digits := j > k
	if j < len(d) && d[j] == '.' {
		j++
		f := j
		for j < len(d) && d[j] >= '0' && d[j] <= '9' {
			j++
		}
		digits = digits || j > f
	}
	if !digits {
		return 0, 0, false
	}
	if j < len(d) && (d[j] == 'e' || d[j] == 'E') {
		k := j + 1
		if k < len(d) && (d[k] == '+' || d[k] == '-') {
			k++
		}
		if k < len(d) && d[k] >= '0' && d[k] <= '9' {
			for k < len(d) && d[k] >= '0' && d[k] <= '9' {
				k++
			}
			j = k
		}
	}
	n, _, ok := scanNumber(d[i:j], 0)
	return n, j - i, ok
}

// arcSegs converts an elliptical arc (SVG 1.1, F.6.5) to cubic curves.
func arcSegs(p0 Point, rx, ry, rot float64, large, sweep bool, p1 Point) []Seg {
	if p0 == p1 {
		return nil
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		return []Seg{{Op: 'L', P: [3]Point{p1}}}
	}
	phi := rot * math.Pi / 180
	sp, cp := math.Sincos(phi)
	dx, dy := (p0.X-p1.X)/2, (p0.Y-p1.Y)/2
	x1, y1 := cp*dx+sp*dy, -sp*dx+cp*dy
	if l := x1*x1/(rx*rx) + y1*y1/(ry*ry); l > 1 {
		s := math.Sqrt(l)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1*y1 - ry*ry*x1*x1
	den := rx*rx*y1*y1 + ry*ry*x1*x1
	co := 0.0
	if den != 0 && num > 0 {
		co = math.Sqrt(num / den)
	}
	if large == sweep {
		co = -co
	}
	cxp, cyp := co*rx*y1/ry, -co*ry*x1/rx
	cx, cy := cp*cxp-sp*cyp+(p0.X+p1.X)/2, sp*cxp+cp*cyp+(p0.Y+p1.Y)/2
	ang := func(ux, uy, vx, vy float64) float64 {
		return math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
	}
	th1 := ang(1, 0, (x1-cxp)/rx, (y1-cyp)/ry)
	dth := ang((x1-cxp)/rx, (y1-cyp)/ry, (-x1-cxp)/rx, (-y1-cyp)/ry)
	if !sweep && dth > 0 {
		dth -= 2 * math.Pi
	} else if sweep && dth < 0 {
		dth += 2 * math.Pi
	}
	n := int(math.Ceil(math.Abs(dth) / (math.Pi / 2)))
	if n < 1 {
		n = 1
	}
	step := dth / float64(n)
	t := 4.0 / 3 * math.Tan(step/4)
	var out []Seg
	pt := func(a float64) (Point, Point) { // point and derivative direction
		sa, ca := math.Sincos(a)
		return Point{cx + rx*ca*cp - ry*sa*sp, cy + rx*ca*sp + ry*sa*cp},
			Point{-rx*sa*cp - ry*ca*sp, -rx*sa*sp + ry*ca*cp}
	}
	for k := 0; k < n; k++ {
		a0, a1 := th1+float64(k)*step, th1+float64(k+1)*step
		q0, d0 := pt(a0)
		q1, d1 := pt(a1)
		if k == n-1 {
			q1 = p1
		}
		out = append(out, Seg{Op: 'C', P: [3]Point{
			{q0.X + t*d0.X, q0.Y + t*d0.Y}, {q1.X - t*d1.X, q1.Y - t*d1.Y}, q1}})
	}
	return out
}

// Walk is a path sampled by arc length: it answers the position, and the
// direction of travel, at a fraction of its length (animateMotion).
type Walk struct {
	pts []Point
	cum []float64
}

// maxWalkPoints bounds the polyline a path is flattened to.
const maxWalkPoints = 1 << 14

// NewWalk flattens the path (each curve into 24 steps) and measures it.
func NewWalk(segs []Seg) *Walk {
	w := &Walk{}
	var cur Point
	var start Point
	add := func(p Point) {
		if len(w.pts) >= maxWalkPoints {
			return
		}
		l := 0.0
		if n := len(w.pts); n > 0 {
			l = w.cum[n-1] + math.Hypot(p.X-w.pts[n-1].X, p.Y-w.pts[n-1].Y)
		}
		w.pts = append(w.pts, p)
		w.cum = append(w.cum, l)
	}
	for _, s := range segs {
		switch s.Op {
		case 'M':
			cur, start = s.P[0], s.P[0]
			if len(w.pts) == 0 {
				add(cur)
			} else {
				add(cur) // a jump inside the path counts no length
				w.cum[len(w.cum)-1] = w.cum[len(w.cum)-2]
			}
		case 'L':
			cur = s.P[0]
			add(cur)
		case 'C':
			for k := 1; k <= 24; k++ {
				t := float64(k) / 24
				u := 1 - t
				add(Point{
					u*u*u*cur.X + 3*u*u*t*s.P[0].X + 3*u*t*t*s.P[1].X + t*t*t*s.P[2].X,
					u*u*u*cur.Y + 3*u*u*t*s.P[0].Y + 3*u*t*t*s.P[1].Y + t*t*t*s.P[2].Y})
			}
			cur = s.P[2]
		case 'Z':
			cur = start
			add(cur)
		}
	}
	return w
}

// Length is the path's length.
func (w *Walk) Length() float64 {
	if len(w.cum) == 0 {
		return 0
	}
	return w.cum[len(w.cum)-1]
}

// At is the point at fraction p of the length and the direction of travel
// there, in degrees.
func (w *Walk) At(p float64) (x, y, angle float64) {
	n := len(w.pts)
	if n == 0 {
		return 0, 0, 0
	}
	if n == 1 || w.Length() == 0 {
		return w.pts[0].X, w.pts[0].Y, 0
	}
	p = math.Max(0, math.Min(1, p))
	target := p * w.Length()
	lo, hi := 0, n-1
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if w.cum[mid] <= target {
			lo = mid
		} else {
			hi = mid
		}
	}
	for lo < n-2 && w.cum[lo+1] == w.cum[lo] {
		lo++ // skip a jump
	}
	a, b := w.pts[lo], w.pts[lo+1]
	seg := w.cum[lo+1] - w.cum[lo]
	f := 0.0
	if seg > 0 {
		f = (target - w.cum[lo]) / seg
	}
	return a.X + (b.X-a.X)*f, a.Y + (b.Y-a.Y)*f, math.Atan2(b.Y-a.Y, b.X-a.X) * 180 / math.Pi
}
