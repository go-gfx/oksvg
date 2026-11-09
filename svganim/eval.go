package svganim

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// State is what animations make of one element at a time: the attributes and
// properties they set, and its transform.
type State struct {
	Attrs     map[string]Value
	Transform *Matrix // the element's effective transform, when an animation drives it
}

// BBoxFunc gives the bounding box of an element's geometry in its own user
// space (the one its transform maps into its parent's), for a CSS
// transform-box of fill-box.
type BBoxFunc func(e int) (x, y, w, h float64, ok bool)

// Eval is the state of every animated element at time t, in seconds.
func (a *Animations) Eval(t float64, bbox BBoxFunc) map[int]*State {
	order := make([]int, len(a.Tracks))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(x, y int) bool { return a.Tracks[order[x]].Begin < a.Tracks[order[y]].Begin })
	type tacc struct {
		m   Matrix
		mot *Matrix
		set bool
	}
	trans := map[int]*tacc{}
	out := map[int]*State{}
	st := func(e int) *State {
		s := out[e]
		if s == nil {
			s = &State{Attrs: map[string]Value{}}
			out[e] = s
		}
		return s
	}
	for _, i := range order {
		tr := &a.Tracks[i]
		p, active := tr.timeAt(t)
		if !active {
			continue
		}
		if tr.Name == "motion" || tr.Name == "transform" {
			acc := trans[tr.Element]
			if acc == nil {
				acc = &tacc{m: a.OwnTransform(tr.Element)}
				trans[tr.Element] = acc
			}
			switch {
			case tr.Name == "motion":
				x, y, ang, ok := tr.motionAt(p)
				if !ok {
					continue
				}
				mm := Translate(x, y).Mul(Rotate(ang))
				if acc.mot != nil {
					mm = acc.mot.Mul(mm)
				}
				acc.mot, acc.set = &mm, true
			case tr.Type != "":
				v, ok := tr.valueAt(p)
				if !ok {
					continue
				}
				m := typeMatrix(tr.Type, v.Nums)
				if tr.Sum {
					acc.m = acc.m.Mul(m)
				} else {
					acc.m = m
				}
				acc.set = true
			default: // the CSS transform
				v, ok := tr.valueAt(p)
				if !ok {
					continue
				}
				m, ok := ParseTransform(v.String())
				if !ok {
					continue
				}
				ox, oy := a.origin(tr, bbox)
				acc.m = m.About(ox, oy)
				acc.set = true
			}
			continue
		}
		v, ok := tr.valueAt(p)
		if !ok {
			continue
		}
		s := st(tr.Element)
		if tr.Sum {
			cur, have := s.Attrs[tr.Name]
			if !have {
				cur, have = a.baseValue(tr.Element, tr.Name)
			}
			if have {
				if sum, ok := cur.Add(v); ok {
					v = sum
				}
			}
		}
		s.Attrs[tr.Name] = v
	}
	for e, acc := range trans {
		if !acc.set {
			continue
		}
		m := acc.m
		if acc.mot != nil {
			m = m.Mul(*acc.mot)
		}
		st(e).Transform = &m
	}
	return out
}

// viewBox is the root's viewBox, or 0 0 with its width and height.
func (a *Animations) viewBox() (x, y, w, h float64) {
	if len(a.Elements) == 0 {
		return 0, 0, 0, 0
	}
	root := a.Elements[0].Attrs
	if vb := ParseValue(root["viewBox"]); len(vb.Nums) == 4 {
		return vb.Nums[0], vb.Nums[1], vb.Nums[2], vb.Nums[3]
	}
	wv, hv := ParseValue(root["width"]), ParseValue(root["height"])
	if len(wv.Nums) > 0 && len(hv.Nums) > 0 {
		return 0, 0, wv.Nums[0], hv.Nums[0]
	}
	return 0, 0, 0, 0
}

// origin is the point a CSS transform pivots on, in the element's user space.
func (a *Animations) origin(tr *Track, bbox BBoxFunc) (float64, float64) {
	o := tr.Origin
	bx, by, bw, bh := a.viewBox()
	if o.FillBox && bbox != nil {
		if x, y, w, h, ok := bbox(tr.Element); ok {
			bx, by, bw, bh = x, y, w, h
		}
	}
	ox, oy := o.X, o.Y
	if o.XPct {
		ox = o.X / 100 * bw
	}
	if o.YPct {
		oy = o.Y / 100 * bh
	}
	return bx + ox, by + oy
}

// typeMatrix is the matrix of an animateTransform value.
func typeMatrix(typ string, n []float64) Matrix {
	switch typ {
	case "translate":
		return Translate(n[0], n[1])
	case "scale":
		return Scale(n[0], n[1])
	case "rotate":
		return Rotate(n[0]).About(n[1], n[2])
	case "skewX":
		return Matrix{1, 0, math.Tan(n[0] * math.Pi / 180), 1, 0, 0}
	case "skewY":
		return Matrix{1, math.Tan(n[0] * math.Pi / 180), 0, 1, 0, 0}
	}
	return Identity
}

// timeAt is where the track is at time t: p is the position in its simple
// duration, 0 to 1, after the direction is applied; active is false where the
// track does not apply (not begun, or ended without a freeze).
func (tr *Track) timeAt(t float64) (p float64, active bool) {
	e := t - tr.Begin
	if e < 0 {
		if !tr.Backwards {
			return 0, false
		}
		e = 0
	}
	ad := tr.ActiveDur()
	const eps = 1e-9
	if math.IsInf(tr.Dur, 1) { // a set with no dur: the value holds for the active duration
		if e >= ad && !math.IsInf(ad, 1) && !tr.Freeze {
			return 0, false
		}
		return 0, true
	}
	var iter float64
	if e >= ad-eps {
		if !tr.Freeze {
			return 0, false
		}
		n := ad / tr.Dur
		if math.Abs(n-math.Round(n)) < 1e-9 {
			iter, p = math.Round(n)-1, 1
		} else {
			iter = math.Floor(n)
			p = n - iter
		}
	} else {
		iter = math.Floor((e + eps) / tr.Dur)
		p = (e - iter*tr.Dur) / tr.Dur
		if p < 0 {
			p = 0
		}
	}
	if iter < 0 {
		iter = 0
	}
	odd := int64(iter)%2 == 1
	rev := tr.Direction == Reverse || tr.Direction == Alternate && odd || tr.Direction == AlternateReverse && !odd
	if rev {
		p = 1 - p
	}
	return math.Max(0, math.Min(1, p)), true
}

// interval finds the pair of keyframes p lies between.
func (tr *Track) interval(p float64) (i int, q float64) {
	kt := tr.KeyTimes
	n := len(kt)
	if n < 2 {
		return 0, 0
	}
	i = sort.SearchFloat64s(kt, p) // first kt >= p
	if i < n && kt[i] == p {
		if i == n-1 {
			i--
			return i, 1
		}
		return i, 0
	}
	i--
	if i < 0 {
		return 0, 0
	}
	if i >= n-1 {
		return n - 2, 1
	}
	span := kt[i+1] - kt[i]
	if span <= 0 {
		return i, 1
	}
	return i, (p - kt[i]) / span
}

// valueAt is the track's value at position p of its simple duration.
func (tr *Track) valueAt(p float64) (Value, bool) {
	n := len(tr.Values)
	switch {
	case n == 0:
		return Value{}, false
	case n == 1:
		return tr.Values[0], true
	}
	i, q := tr.interval(p)
	if tr.Calc == Discrete {
		// the last keyframe at or before p holds
		j := sort.Search(n, func(k int) bool { return tr.KeyTimes[k] > p }) - 1
		if j < 0 {
			j = 0
		}
		return tr.Values[j], true
	}
	if i < len(tr.Easing) {
		q = tr.Easing[i].Apply(q)
	}
	v, ok := tr.Values[i].Lerp(tr.Values[i+1], q)
	if !ok { // not interpolable: the from value for the first half, the to value for the rest
		if q < 0.5 {
			return tr.Values[i], true
		}
		return tr.Values[i+1], true
	}
	return v, true
}

// motionAt is the offset of an animateMotion at position p, and the turn it
// asks for.
func (tr *Track) motionAt(p float64) (x, y, angle float64, ok bool) {
	if tr.Motion == nil {
		v, ok := tr.valueAt(p)
		if !ok || len(v.Nums) < 2 {
			return 0, 0, 0, false
		}
		return v.Nums[0], v.Nums[1], 0, true
	}
	f := p
	if kp := tr.Motion.KeyPoints; len(kp) >= 2 && len(kp) == len(tr.KeyTimes) {
		i, q := tr.interval(p)
		if i < len(tr.Easing) {
			q = tr.Easing[i].Apply(q)
		}
		f = kp[i] + (kp[i+1]-kp[i])*q
	}
	x, y, ang := tr.Motion.Walk.At(f)
	switch tr.Motion.Rotate {
	case RotAuto:
		angle = ang
	case RotAutoReverse:
		angle = ang + 180
	case RotAngle:
		angle = tr.Motion.Angle
	}
	return x, y, angle, true
}

// Apply is the progress q (0 to 1) shaped by the easing.
func (e Easing) Apply(q float64) float64 {
	if e.Steps > 0 {
		n := float64(e.Steps)
		var s float64
		if e.StepStart {
			s = math.Ceil(q*n) / n
		} else {
			s = math.Floor(q*n) / n
		}
		return math.Max(0, math.Min(1, s))
	}
	if !e.set || e.Spline == [4]float64{0, 0, 1, 1} {
		return q
	}
	return bezierY(e.Spline, q)
}

// bezierY solves the cubic Bézier of (0,0), (x1,y1), (x2,y2), (1,1) for the
// y at x (CSS Easing Functions Level 1, 2.1).
func bezierY(c [4]float64, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	bez := func(a, b, t float64) float64 { // the 1-D curve with controls a and b
		u := 1 - t
		return 3*u*u*t*a + 3*u*t*t*b + t*t*t
	}
	lo, hi := 0.0, 1.0
	t := x
	for i := 0; i < 40; i++ {
		v := bez(c[0], c[2], t)
		if math.Abs(v-x) < 1e-7 {
			break
		}
		if v < x {
			lo = t
		} else {
			hi = t
		}
		t = (lo + hi) / 2
	}
	return bez(c[1], c[3], t)
}

// Span is the stretch of time to show an animation over.
type Span struct {
	End    float64 // seconds: where every finite animation has ended, or one loop of the infinite ones
	Loops  bool    // some animation never stops: the span repeats
	Delay  bool    // an infinite animation begins after 0: a loop replays the delay
	Approx bool    // the loops' periods have no common period within the limit: the longest is used
}

// Span computes the timeline's extent: the end of the finite animations, and
// for the infinite ones the least common multiple of their periods (bounded by
// Limits.MaxDuration; past it the longest period stands and Approx is set).
func (a *Animations) Span() Span {
	var s Span
	var periods []int64
	maxBegin := 0.0
	for i := range a.Tracks {
		tr := &a.Tracks[i]
		if tr.Infinite() {
			s.Loops = true
			per := tr.Dur
			if tr.Direction == Alternate || tr.Direction == AlternateReverse {
				per *= 2
			}
			if math.IsInf(per, 1) {
				continue // a set that holds: no period
			}
			periods = append(periods, int64(math.Round(per*1000)))
			if tr.Begin > 0 {
				s.Delay = true
				maxBegin = math.Max(maxBegin, tr.Begin)
			}
			continue
		}
		s.End = math.Max(s.End, tr.Begin+tr.ActiveDur())
	}
	if len(periods) > 0 {
		l := int64(1)
		var longest int64
		for _, p := range periods {
			if p <= 0 {
				continue
			}
			longest = max(longest, p)
			g := gcd(l, p)
			if l/g > int64(a.lim.MaxDuration*1000)/p+1 {
				l = -1
				break
			}
			l = l / g * p
		}
		if l < 0 || float64(l)/1000 > a.lim.MaxDuration {
			l = longest
			s.Approx = true
		}
		s.End = math.Max(s.End, maxBegin+float64(l)/1000)
	}
	if s.End > a.lim.MaxDuration {
		s.End = a.lim.MaxDuration
		s.Approx = true
	}
	return s
}

func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// FrameTimes are the times to draw the animation at: fps frames a second over
// the span, at most Limits.MaxFrames of them (the rate is lowered to fit). The
// second result is the time between two frames, in seconds.
func (a *Animations) FrameTimes(s Span, fps float64) ([]float64, float64) {
	if fps <= 0 {
		fps = 12
	}
	end := math.Max(s.End, 1/fps)
	n := int(math.Ceil(end * fps))
	if n > a.lim.MaxFrames {
		n = a.lim.MaxFrames
	}
	if n < 1 {
		n = 1
	}
	step := end / float64(n)
	ts := make([]float64, n)
	for i := range ts {
		ts[i] = float64(i) * step
	}
	return ts, step
}

// formatTime is for messages.
func formatTime(s float64) string { return strconv.FormatFloat(s, 'f', -1, 64) + "s" }

// Describe is a one-line account of a track, for a warning.
func (tr *Track) Describe() string {
	what := tr.Name
	if tr.Type != "" {
		what += " " + tr.Type
	}
	var b strings.Builder
	b.WriteString(what + " (" + tr.Source.String() + ", " + formatTime(tr.Dur))
	if tr.Infinite() {
		b.WriteString(", repeating")
	}
	b.WriteString(")")
	return b.String()
}
