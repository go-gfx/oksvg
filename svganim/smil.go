package svganim

import (
	"math"
	"strconv"
	"strings"
)

// syncRef is a begin or end that is another animation's begin or end plus an
// offset (SMIL Animation, 5.4.1 "syncbase-value"), settled once every
// animation is read.
type syncRef struct {
	track int
	id    string
	end   bool // the syncbase's end, else its begin
	off   float64
	isEnd bool // it is this track's end that is relative, else its begin
}

// parseClock reads a SMIL clock value (SMIL Animation, 3.2.2): full or partial
// clock ("00:01:02.5", "01:02"), or a timecount with an optional metric (h,
// min, s, ms; seconds when none).
func parseClock(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	sign := 1.0
	if s[0] == '+' || s[0] == '-' {
		if s[0] == '-' {
			sign = -1
		}
		s = strings.TrimSpace(s[1:])
	}
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return 0, false
		}
		var sec float64
		for _, p := range parts {
			f, err := strconv.ParseFloat(p, 64)
			if err != nil || f < 0 {
				return 0, false
			}
			sec = sec*60 + f
		}
		return sign * sec, true
	}
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "ms"):
		mult, s = 0.001, s[:len(s)-2]
	case strings.HasSuffix(s, "min"):
		mult, s = 60, s[:len(s)-3]
	case strings.HasSuffix(s, "h"):
		mult, s = 3600, s[:len(s)-1]
	case strings.HasSuffix(s, "s"):
		s = s[:len(s)-1]
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return sign * f * mult, true
}

// beginSpec is the first usable entry of a begin list.
type beginSpec struct {
	t        float64
	sync     *syncRef // set when the time is another animation's
	event    string   // the unsupported trigger, when that is all there is
	never    bool
	multiple bool
}

func parseBegin(s string) beginSpec {
	var b beginSpec
	items := strings.Split(s, ";")
	first := true
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		if !first {
			b.multiple = true
			continue
		}
		first = false
		if it == "indefinite" {
			b.never = true
			continue
		}
		if t, ok := parseClock(it); ok {
			b.t = t
			continue
		}
		// id.begin+1s, id.end, id.click, id.repeat(2), accessKey(x), wallclock(...)
		if dot := strings.IndexByte(it, '.'); dot > 0 {
			id := it[:dot]
			rest := it[dot+1:]
			off := 0.0
			ev := rest
			if k := strings.IndexAny(rest, "+-"); k > 0 {
				ev = strings.TrimSpace(rest[:k])
				off, _ = parseClock(rest[k:])
			}
			if ev == "begin" || ev == "end" {
				b.sync = &syncRef{id: id, end: ev == "end", off: off}
				continue
			}
			b.event = it
			continue
		}
		b.event = it
	}
	return b
}

func (a *Animations) parseSMIL(elem, target int, name string, at map[string]string, ids map[string]int, mpathD string) {
	tr := Track{
		Element: target, ID: a.Elements[target].ID, Source: SMIL,
		Dur: math.Inf(1), Repeat: 1, End: math.Inf(1), animID: at["id"],
	}
	label := "<" + name + ">"
	if id := at["id"]; id != "" {
		label += " #" + id
	}
	b := parseBegin(at["begin"])
	switch {
	case b.never:
		a.warn("%s starts only when a script begins it: left out", label)
		return
	case b.event != "":
		a.warn("%s starts on %q, an event: left out (an exported slide has no pointer events)", label, b.event)
		return
	}
	if b.multiple {
		a.warn("%s has several begin times: only the first is kept", label)
	}
	tr.Begin = b.t
	if d := strings.TrimSpace(at["dur"]); d != "" && d != "indefinite" && d != "media" {
		v, ok := parseClock(d)
		if !ok || v <= 0 {
			a.warn("%s has the duration %q: left out", label, d)
			return
		}
		tr.Dur = v
	}
	switch rc := strings.TrimSpace(at["repeatCount"]); rc {
	case "":
	case "indefinite":
		tr.Repeat = math.Inf(1)
	default:
		f, err := strconv.ParseFloat(rc, 64)
		if err != nil || f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
			a.warn("%s has the repeatCount %q: taken as 1", label, rc)
		} else {
			tr.Repeat = f
		}
	}
	switch rd := strings.TrimSpace(at["repeatDur"]); rd {
	case "":
	case "indefinite":
		tr.RepeatDur = math.Inf(1)
	default:
		if v, ok := parseClock(rd); ok && v > 0 {
			tr.RepeatDur = v
		}
	}
	var endSync *syncRef
	if e := strings.TrimSpace(at["end"]); e != "" && e != "indefinite" {
		eb := parseBegin(e)
		switch {
		case eb.sync != nil:
			endSync = eb.sync
		case eb.event != "":
			a.warn("%s ends on %q, an event: it is taken as never ending", label, eb.event)
		default:
			tr.End = eb.t
		}
	}
	tr.Freeze = strings.TrimSpace(at["fill"]) == "freeze"
	tr.Sum = strings.TrimSpace(at["additive"]) == "sum"
	if strings.TrimSpace(at["accumulate"]) == "sum" {
		a.warn("%s accumulates (accumulate=\"sum\"): each repeat starts again from the first value", label)
	}
	switch strings.TrimSpace(at["calcMode"]) {
	case "discrete":
		tr.Calc = Discrete
	case "paced":
		tr.Calc = Paced
	case "spline":
	default:
		if name == "animateMotion" {
			tr.Calc = Paced
		}
	}
	calcMode := strings.TrimSpace(at["calcMode"])

	attr := strings.TrimSpace(at["attributeName"])
	attr = strings.TrimPrefix(strings.TrimPrefix(attr, "CSS:"), "XML:")
	switch name {
	case "animateTransform":
		attr = "transform"
		tr.Type = strings.TrimSpace(at["type"])
		if tr.Type == "" {
			tr.Type = "translate"
		}
	case "animateMotion":
		attr = "motion"
	case "animate", "animateColor", "set":
		if attr == "" {
			a.warn("%s names no attributeName: left out", label)
			return
		}
		if attr == "transform" {
			a.warn("%s animates transform: SVG asks for animateTransform; left out", label)
			return
		}
	}
	tr.Name = attr

	// values
	var raw []string
	switch {
	case at["values"] != "":
		for _, v := range strings.Split(at["values"], ";") {
			if v = strings.TrimSpace(v); v != "" {
				raw = append(raw, v)
			}
		}
	case name == "set":
		raw = []string{at["to"]}
	default:
		from, to, by := strings.TrimSpace(at["from"]), strings.TrimSpace(at["to"]), strings.TrimSpace(at["by"])
		switch {
		case from != "" && to != "":
			raw = []string{from, to}
		case from != "" && by != "":
			raw = []string{from, "\x00by" + by}
		case to != "":
			raw = []string{"\x00base", to}
		case by != "":
			raw = []string{"\x00zero", by}
			tr.Sum = true
		}
	}
	if len(raw) > a.lim.MaxKeyframes {
		a.warn("%s has %d values: past the limit of %d, left out", label, len(raw), a.lim.MaxKeyframes)
		return
	}
	if name == "animateMotion" {
		a.parseMotion(&tr, at, raw, mpathD, label)
		if tr.Motion == nil && len(tr.Values) == 0 {
			return
		}
	} else {
		if len(raw) == 0 || (name != "set" && len(raw) < 2) {
			a.warn("%s has no from/to or values: left out", label)
			return
		}
		base, hasBase := a.baseValue(target, attr)
		for _, r := range raw {
			switch {
			case r == "\x00base":
				if !hasBase {
					a.warn("%s animates %s from its current value, which the document does not give: left out", label, attr)
					return
				}
				tr.Values = append(tr.Values, base)
			case r == "\x00zero":
				tr.Values = append(tr.Values, zeroLike(ParseValue(strings.TrimSpace(at["by"]))))
			case strings.HasPrefix(r, "\x00by"):
				f := tr.Values[0]
				sum, ok := f.Add(ParseValue(r[3:]))
				if !ok {
					a.warn("%s: by cannot be added to from: left out", label)
					return
				}
				tr.Values = append(tr.Values, sum)
			default:
				tr.Values = append(tr.Values, ParseValue(r))
			}
		}
		if name == "animateTransform" {
			if !a.padTransform(&tr, label) {
				return
			}
		}
		if name == "set" {
			tr.Calc = Discrete
		}
	}
	n := len(tr.Values)
	if tr.Motion != nil {
		n = len(raw)
	}
	// keyTimes
	if kt := strings.TrimSpace(at["keyTimes"]); kt != "" && n > 1 {
		var ts []float64
		okk := true
		for _, p := range strings.Split(kt, ";") {
			if p = strings.TrimSpace(p); p == "" {
				continue
			}
			f, err := strconv.ParseFloat(p, 64)
			if err != nil || f < 0 || f > 1 || math.IsNaN(f) {
				okk = false
			}
			ts = append(ts, f)
		}
		for i := 1; i < len(ts); i++ {
			if ts[i] < ts[i-1] {
				okk = false
			}
		}
		if okk && len(ts) == n && ts[0] == 0 && (tr.Calc == Discrete || ts[n-1] == 1) {
			tr.KeyTimes = ts
		} else if tr.Calc != Paced {
			a.warn("%s has keyTimes that do not fit its %d values: they are ignored", label, n)
		}
	}
	if calcMode == "spline" && n > 1 {
		ks := strings.Split(at["keySplines"], ";")
		var es []Easing
		for _, k := range ks {
			f := strings.FieldsFunc(k, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
			if len(f) == 0 {
				continue
			}
			if len(f) != 4 {
				es = nil
				break
			}
			var e Easing
			for i := range f {
				v, err := strconv.ParseFloat(f[i], 64)
				if err != nil || v < 0 && (i == 0 || i == 2) || v > 1 && (i == 0 || i == 2) {
					es = nil
					break
				}
				e.Spline[i] = v
			}
			e.set = true
			es = append(es, e)
		}
		if len(es) == n-1 {
			tr.Easing = es
		} else {
			a.warn("%s has keySplines that do not fit its %d values: linear is used", label, n)
		}
	}
	tr.finishTimes(n)
	if endSync != nil {
		endSync.isEnd = true
		endSync.track = len(a.Tracks)
		a.sync = append(a.sync, *endSync)
	}
	if b.sync != nil {
		b.sync.track = len(a.Tracks)
		a.sync = append(a.sync, *b.sync)
	}
	a.Tracks = append(a.Tracks, tr)
}

// finishTimes fills the key times a track leaves implicit: evenly spaced (one
// per value for a discrete animation, the first and last at 0 and 1 for the
// others), and by distance for paced.
func (t *Track) finishTimes(n int) {
	if len(t.KeyTimes) == n || n < 2 {
		if n < 2 && len(t.KeyTimes) == 0 {
			t.KeyTimes = make([]float64, n)
		}
		return
	}
	t.KeyTimes = make([]float64, n)
	switch {
	case t.Calc == Discrete:
		for i := range t.KeyTimes {
			t.KeyTimes[i] = float64(i) / float64(n)
		}
	case t.Calc == Paced && t.Motion == nil:
		total := 0.0
		d := make([]float64, n)
		for i := 1; i < n; i++ {
			d[i] = d[i-1] + t.Values[i-1].Dist(t.Values[i])
		}
		total = d[n-1]
		for i := range t.KeyTimes {
			if total > 0 {
				t.KeyTimes[i] = d[i] / total
			} else {
				t.KeyTimes[i] = float64(i) / float64(n-1)
			}
		}
		t.Calc = Linear
	default:
		for i := range t.KeyTimes {
			t.KeyTimes[i] = float64(i) / float64(n-1)
		}
	}
}

// zeroLike is the value with every number 0: the start of a by animation.
func zeroLike(v Value) Value {
	if v.Color != nil {
		c := *v.Color
		c.R, c.G, c.B = 0, 0, 0
		return Value{Color: &c}
	}
	return Value{Skel: v.Skel, Nums: make([]float64, len(v.Nums))}
}

// padTransform brings an animateTransform's values to the full arity of its
// type: translate x y, scale x y, rotate angle cx cy, skewX/skewY angle.
func (a *Animations) padTransform(tr *Track, label string) bool {
	arity := map[string]int{"translate": 2, "scale": 2, "rotate": 3, "skewX": 1, "skewY": 1}[tr.Type]
	if arity == 0 {
		a.warn("%s has the type %q: left out", label, tr.Type)
		return false
	}
	for i, v := range tr.Values {
		nums := v.Nums
		if v.Color != nil || len(nums) == 0 || len(nums) > arity || (tr.Type == "rotate" && len(nums) == 2) {
			a.warn("%s has the %s value %q: left out", label, tr.Type, v.String())
			return false
		}
		out := make([]float64, arity)
		copy(out, nums)
		if len(nums) == 1 && tr.Type == "scale" {
			out[1] = nums[0]
		}
		skel := make([]string, arity+1)
		skel[0] = ""
		for k := 1; k < arity; k++ {
			skel[k] = " "
		}
		tr.Values[i] = Value{Skel: skel, Nums: out}
	}
	return true
}

// parseMotion reads an animateMotion's path: mpath, else path, else values,
// else from, to and by points.
func (a *Animations) parseMotion(tr *Track, at map[string]string, raw []string, mpathD, label string) {
	d := mpathD
	if d == "" {
		d = at["path"]
	}
	if d != "" {
		segs, ok := ParsePath(d)
		if !ok {
			a.warn("%s has a path that does not parse: left out", label)
			return
		}
		tr.Motion = &Motion{Segs: segs, Walk: NewWalk(segs)}
	} else {
		if len(raw) < 2 {
			a.warn("%s has no path, values or from/to: left out", label)
			return
		}
		for _, r := range raw {
			switch {
			case r == "\x00zero":
				r = "0,0"
			case strings.HasPrefix(r, "\x00by"):
				r = "0,0"
			}
			v := ParseValue(r)
			if len(v.Nums) != 2 {
				a.warn("%s has the point %q: left out", label, r)
				return
			}
			tr.Values = append(tr.Values, Value{Skel: []string{"", " ", ""}, Nums: v.Nums})
		}
	}
	rot := strings.TrimSpace(at["rotate"])
	switch rot {
	case "", "0":
	case "auto":
		tr.Motion = ensureMotion(tr)
		tr.Motion.Rotate = RotAuto
	case "auto-reverse":
		tr.Motion = ensureMotion(tr)
		tr.Motion.Rotate = RotAutoReverse
	default:
		if f, err := strconv.ParseFloat(rot, 64); err == nil {
			tr.Motion = ensureMotion(tr)
			tr.Motion.Rotate, tr.Motion.Angle = RotAngle, f
		}
	}
	if kp := strings.TrimSpace(at["keyPoints"]); kp != "" && tr.Motion != nil {
		for _, p := range strings.Split(kp, ";") {
			f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
			if err != nil || f < 0 || f > 1 {
				a.warn("%s has keyPoints that do not parse: they are ignored", label)
				tr.Motion.KeyPoints = nil
				break
			}
			tr.Motion.KeyPoints = append(tr.Motion.KeyPoints, f)
		}
	}
}

func ensureMotion(tr *Track) *Motion {
	if tr.Motion != nil {
		return tr.Motion
	}
	// a values-based motion keeps its points as Values; the rotation needs the
	// path they make
	var segs []Seg
	for i, v := range tr.Values {
		op := byte('L')
		if i == 0 {
			op = 'M'
		}
		segs = append(segs, Seg{Op: op, P: [3]Point{{v.Nums[0], v.Nums[1]}}})
	}
	return &Motion{Segs: segs, Walk: NewWalk(segs)}
}

// resolveSyncbases settles the begins and ends that are another animation's,
// by id, with a bound on the chain (a cycle is left at 0).
func (a *Animations) resolveSyncbases(ids map[string]int) {
	byID := map[string]int{}
	for i := range a.Tracks {
		if id := a.Tracks[i].animID; id != "" {
			byID[id] = i
		}
	}
	_ = ids
	for round := 0; round < 8; round++ {
		for _, s := range a.sync {
			k, ok := byID[s.id]
			if !ok || k >= len(a.Tracks) || k == s.track {
				continue
			}
			ref := a.Tracks[k]
			t := ref.Begin
			if s.end {
				t = ref.Begin + ref.ActiveDur()
				if math.IsInf(t, 1) {
					continue
				}
			}
			if s.isEnd {
				a.Tracks[s.track].End = t + s.off
			} else {
				a.Tracks[s.track].Begin = t + s.off
			}
		}
	}
	a.sync = nil
}

// baseValue is the attribute's value on the element as the document gives it,
// or the property's initial value.
func (a *Animations) baseValue(e int, name string) (Value, bool) {
	if v, ok := a.Elements[e].Attrs[name]; ok {
		return ParseValue(v), true
	}
	if st, ok := a.Elements[e].Attrs["style"]; ok {
		for _, decl := range strings.Split(st, ";") {
			k, v, ok := strings.Cut(decl, ":")
			if ok && strings.TrimSpace(k) == name {
				return ParseValue(v), true
			}
		}
	}
	switch name {
	case "opacity", "fill-opacity", "stroke-opacity":
		return ParseValue("1"), true
	case "fill":
		return ParseValue("#000000"), true
	case "stroke-width":
		return ParseValue("1"), true
	case "stroke-dashoffset":
		return ParseValue("0"), true
	}
	return Value{}, false
}
