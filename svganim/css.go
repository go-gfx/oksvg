package svganim

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// The CSS animations of an SVG document: @keyframes (CSS Animations Level 1,
// section 4) and the animation properties (section 3) of the elements they
// are applied to, by a rule of a style element or a style attribute. The
// selectors understood are the simple ones a drawing's own style uses: the
// universal, a type, a class, an id and their compounds, in a list.

// maxCSSRules and maxKeyframeSets bound a style sheet.
const (
	maxCSSRules     = 10000
	maxKeyframeSets = 2000
)

type cssDecl struct{ prop, val string }

type cssSel struct {
	tag, id string
	classes []string
}

func (s cssSel) spec() int { return len(s.classes)*10 + btoi(s.id != "")*100 + btoi(s.tag != "") }

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s cssSel) match(e ElementInfo) bool {
	if s.tag != "" && s.tag != e.Name {
		return false
	}
	if s.id != "" && s.id != e.ID {
		return false
	}
	if len(s.classes) > 0 {
		have := strings.Fields(e.Attrs["class"])
		for _, c := range s.classes {
			ok := false
			for _, h := range have {
				if h == c {
					ok = true
					break
				}
			}
			if !ok {
				return false
			}
		}
	}
	return true
}

type cssRule struct {
	sel   cssSel
	decls []cssDecl
	order int
}

type cssFrame struct {
	at    float64
	decls []cssDecl
}

// stripComments removes /* */ comments.
func stripComments(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		j := strings.Index(s[i+2:], "*/")
		if j < 0 {
			return b.String()
		}
		s = s[i+2+j+2:]
	}
}

// block returns the text up to the brace that closes the one just opened, and
// the rest after it.
func block(s string) (body, rest string, ok bool) {
	depth := 1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[:i], s[i+1:], true
			}
		}
	}
	return s, "", false
}

// splitTop splits on sep outside parentheses and quotes.
func splitTop(s string, sep byte) []string {
	var out []string
	depth := 0
	var q byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == sep && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func parseDecls(s string) []cssDecl {
	var out []cssDecl
	for _, d := range splitTop(s, ';') {
		k, v, ok := strings.Cut(d, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		v = strings.TrimSpace(strings.TrimSuffix(v, "!important"))
		if k != "" && v != "" {
			out = append(out, cssDecl{k, v})
		}
	}
	return out
}

func parseSimpleSel(s string) (cssSel, bool) {
	s = strings.TrimSpace(s)
	var sel cssSel
	if s == "" || strings.ContainsAny(s, " >+~:[()*,\t\n") && s != "*" {
		return sel, false
	}
	if s == "*" {
		return sel, true
	}
	i := 0
	for i < len(s) {
		c := s[i]
		j := i + 1
		for j < len(s) && s[j] != '.' && s[j] != '#' {
			j++
		}
		switch c {
		case '.':
			sel.classes = append(sel.classes, s[i+1:j])
		case '#':
			sel.id = s[i+1 : j]
		default:
			if i != 0 {
				return sel, false
			}
			sel.tag = s[i:j]
		}
		i = j
	}
	return sel, true
}

func animDecl(p string) bool { return p == "animation" || strings.HasPrefix(p, "animation-") }

func (a *Animations) parseCSS(text string) {
	if strings.TrimSpace(text) == "" && !a.anyInlineAnimation() {
		return
	}
	text = stripComments(text)
	keyframes := map[string][]cssFrame{}
	var rules []cssRule
	for {
		text = strings.TrimSpace(text)
		if text == "" {
			break
		}
		open := strings.IndexByte(text, '{')
		semi := strings.IndexByte(text, ';')
		if strings.HasPrefix(text, "@") && semi >= 0 && (open < 0 || semi < open) {
			text = text[semi+1:] // @import, @charset
			continue
		}
		if open < 0 {
			break
		}
		head := strings.TrimSpace(text[:open])
		body, rest, ok := block(text[open+1:])
		if !ok {
			break
		}
		text = rest
		if strings.HasPrefix(head, "@") {
			f := strings.Fields(head)
			if len(f) == 2 && (f[0] == "@keyframes" || f[0] == "@-webkit-keyframes") {
				if len(keyframes) < maxKeyframeSets {
					keyframes[strings.Trim(f[1], `"'`)] = a.parseKeyframes(body)
				}
			} else if len(f) > 0 && strings.Contains(body, "animation") {
				a.warn("the at-rule %s holds animations: it is not applied", f[0])
			}
			continue
		}
		decls := parseDecls(body)
		hasAnim := false
		for _, d := range decls {
			if animDecl(d.prop) {
				hasAnim = true
			}
		}
		for _, sl := range strings.Split(head, ",") {
			sel, ok := parseSimpleSel(sl)
			if !ok {
				if hasAnim {
					a.warn("the selector %q is not understood: its animation is left out", strings.TrimSpace(sl))
				}
				continue
			}
			if len(rules) < maxCSSRules {
				rules = append(rules, cssRule{sel: sel, decls: decls, order: len(rules)})
			}
		}
	}
	// index the rules by their most specific part
	byID, byClass, byTag := map[string][]int{}, map[string][]int{}, map[string][]int{}
	var universal []int
	for i, r := range rules {
		switch {
		case r.sel.id != "":
			byID[r.sel.id] = append(byID[r.sel.id], i)
		case len(r.sel.classes) > 0:
			byClass[r.sel.classes[0]] = append(byClass[r.sel.classes[0]], i)
		case r.sel.tag != "":
			byTag[r.sel.tag] = append(byTag[r.sel.tag], i)
		default:
			universal = append(universal, i)
		}
	}
	for ei, e := range a.Elements {
		if animTags[e.Name] || e.Name == "style" {
			continue
		}
		var cand []int
		cand = append(cand, universal...)
		cand = append(cand, byTag[e.Name]...)
		if e.ID != "" {
			cand = append(cand, byID[e.ID]...)
		}
		for _, c := range strings.Fields(e.Attrs["class"]) {
			cand = append(cand, byClass[c]...)
		}
		var hit []int
		for _, i := range cand {
			if rules[i].sel.match(e) {
				hit = append(hit, i)
			}
		}
		sort.SliceStable(hit, func(x, y int) bool {
			sx, sy := rules[hit[x]].sel.spec(), rules[hit[y]].sel.spec()
			if sx != sy {
				return sx < sy
			}
			return rules[hit[x]].order < rules[hit[y]].order
		})
		var decls []cssDecl
		for _, i := range hit {
			decls = append(decls, rules[i].decls...)
		}
		if st := e.Attrs["style"]; st != "" {
			decls = append(decls, parseDecls(st)...)
		}
		has := false
		for _, d := range decls {
			if animDecl(d.prop) {
				has = true
				break
			}
		}
		if !has {
			continue
		}
		a.applyAnimations(ei, decls, keyframes)
	}
}

func (a *Animations) anyInlineAnimation() bool {
	for _, e := range a.Elements {
		if strings.Contains(e.Attrs["style"], "animation") {
			return true
		}
	}
	return false
}

func (a *Animations) parseKeyframes(body string) []cssFrame {
	var out []cssFrame
	for {
		body = strings.TrimSpace(body)
		open := strings.IndexByte(body, '{')
		if open < 0 {
			break
		}
		head := body[:open]
		inner, rest, ok := block(body[open+1:])
		if !ok {
			break
		}
		body = rest
		decls := parseDecls(inner)
		for _, sel := range strings.Split(head, ",") {
			sel = strings.TrimSpace(strings.ToLower(sel))
			var at float64
			switch {
			case sel == "from":
			case sel == "to":
				at = 1
			case strings.HasSuffix(sel, "%"):
				f, err := strconv.ParseFloat(strings.TrimSuffix(sel, "%"), 64)
				if err != nil || f < 0 || f > 100 {
					continue
				}
				at = f / 100
			default:
				continue
			}
			if len(out) < a.lim.MaxKeyframes {
				out = append(out, cssFrame{at: at, decls: decls})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].at < out[j].at })
	return out
}

type animSpec struct {
	name   string
	dur    float64
	delay  float64
	count  float64
	dir    Direction
	fill   string
	timing Easing
	paused bool
}

var timingKeywords = map[string]Easing{
	"linear":      {},
	"ease":        {Spline: [4]float64{0.25, 0.1, 0.25, 1}, set: true},
	"ease-in":     {Spline: [4]float64{0.42, 0, 1, 1}, set: true},
	"ease-out":    {Spline: [4]float64{0, 0, 0.58, 1}, set: true},
	"ease-in-out": {Spline: [4]float64{0.42, 0, 0.58, 1}, set: true},
}

func parseTiming(s string) (Easing, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if e, ok := timingKeywords[s]; ok {
		return e, true
	}
	switch s {
	case "step-start":
		return Easing{Steps: 1, StepStart: true}, true
	case "step-end":
		return Easing{Steps: 1}, true
	}
	if args, ok := funcArgs(s, "cubic-bezier"); ok {
		f := strings.Split(args, ",")
		if len(f) != 4 {
			return Easing{}, false
		}
		var e Easing
		for i := range f {
			v, err := strconv.ParseFloat(strings.TrimSpace(f[i]), 64)
			if err != nil || (i == 0 || i == 2) && (v < 0 || v > 1) {
				return Easing{}, false
			}
			e.Spline[i] = v
		}
		e.set = true
		return e, true
	}
	if args, ok := funcArgs(s, "steps"); ok {
		f := strings.Split(args, ",")
		n, err := strconv.Atoi(strings.TrimSpace(f[0]))
		if err != nil || n < 1 || n > 1000 {
			return Easing{}, false
		}
		e := Easing{Steps: n}
		if len(f) > 1 {
			switch strings.TrimSpace(f[1]) {
			case "start", "jump-start":
				e.StepStart = true
			case "end", "jump-end":
			default:
				return Easing{}, false
			}
		}
		return e, true
	}
	return Easing{}, false
}

func funcArgs(s, name string) (string, bool) {
	if !strings.HasPrefix(s, name+"(") || !strings.HasSuffix(s, ")") {
		return "", false
	}
	return s[len(name)+1 : len(s)-1], true
}

func cssTime(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch {
	case strings.HasSuffix(s, "ms"):
		f, err := strconv.ParseFloat(s[:len(s)-2], 64)
		return f / 1000, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	case strings.HasSuffix(s, "s"):
		f, err := strconv.ParseFloat(s[:len(s)-1], 64)
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	return 0, false
}

func newSpec() animSpec {
	return animSpec{dur: 0, count: 1, timing: timingKeywords["ease"], fill: "none"}
}

func parseDirection(s string) (Direction, bool) {
	switch s {
	case "normal":
		return Normal, true
	case "reverse":
		return Reverse, true
	case "alternate":
		return Alternate, true
	case "alternate-reverse":
		return AlternateReverse, true
	}
	return Normal, false
}

// specs reads the animation properties of a cascade into the list of
// animations the element has.
func (a *Animations) specs(decls []cssDecl) []animSpec {
	var list []animSpec
	setAll := func(vals string, f func(sp *animSpec, v string)) {
		items := splitTop(vals, ',')
		for len(list) < len(items) {
			list = append(list, newSpec())
		}
		for i := range list {
			f(&list[i], strings.TrimSpace(items[i%len(items)]))
		}
	}
	for _, d := range decls {
		switch d.prop {
		case "animation":
			list = nil
			if d.val == "none" {
				continue
			}
			for _, item := range splitTop(d.val, ',') {
				sp := newSpec()
				timeSeen := 0
				for _, tok := range splitTop(strings.Join(strings.Fields(item), " "), ' ') {
					tok = strings.TrimSpace(tok)
					low := strings.ToLower(tok)
					if tok == "" {
						continue
					}
					if t, ok := cssTime(low); ok {
						if timeSeen == 0 {
							sp.dur = t
						} else {
							sp.delay = t
						}
						timeSeen++
						continue
					}
					if e, ok := parseTiming(low); ok {
						sp.timing = e
						continue
					}
					if low == "infinite" {
						sp.count = math.Inf(1)
						continue
					}
					if f, err := strconv.ParseFloat(low, 64); err == nil && f >= 0 {
						sp.count = f
						continue
					}
					if dir, ok := parseDirection(low); ok {
						sp.dir = dir
						continue
					}
					switch low {
					case "forwards", "backwards", "both":
						sp.fill = low
						continue
					case "running":
						continue
					case "paused":
						sp.paused = true
						continue
					case "none":
						continue
					}
					sp.name = strings.Trim(tok, `"'`)
				}
				list = append(list, sp)
			}
		case "animation-name":
			setAll(d.val, func(sp *animSpec, v string) {
				sp.name = strings.Trim(v, `"'`)
				if v == "none" {
					sp.name = ""
				}
			})
		case "animation-duration":
			setAll(d.val, func(sp *animSpec, v string) { sp.dur, _ = cssTime(v) })
		case "animation-delay":
			setAll(d.val, func(sp *animSpec, v string) { sp.delay, _ = cssTime(v) })
		case "animation-timing-function":
			setAll(d.val, func(sp *animSpec, v string) {
				if e, ok := parseTiming(v); ok {
					sp.timing = e
				}
			})
		case "animation-iteration-count":
			setAll(d.val, func(sp *animSpec, v string) {
				if v == "infinite" {
					sp.count = math.Inf(1)
				} else if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
					sp.count = f
				}
			})
		case "animation-direction":
			setAll(d.val, func(sp *animSpec, v string) { sp.dir, _ = parseDirection(v) })
		case "animation-fill-mode":
			setAll(d.val, func(sp *animSpec, v string) { sp.fill = v })
		case "animation-play-state":
			setAll(d.val, func(sp *animSpec, v string) { sp.paused = v == "paused" })
		}
	}
	return list
}

// cssAnimatable are the properties of an SVG element a keyframe may animate
// here: the paint and geometry attributes, which interpolate as values, and
// the transform.
var cssAnimatable = map[string]bool{
	"opacity": true, "fill": true, "stroke": true, "fill-opacity": true, "stroke-opacity": true,
	"stroke-width": true, "stroke-dashoffset": true, "stroke-dasharray": true,
	"stop-color": true, "stop-opacity": true, "flood-color": true,
	"r": true, "cx": true, "cy": true, "x": true, "y": true, "width": true, "height": true, "rx": true, "ry": true,
	"d": true, "transform": true, "visibility": true,
}

func (a *Animations) applyAnimations(ei int, decls []cssDecl, keyframes map[string][]cssFrame) {
	e := a.Elements[ei]
	label := "<" + e.Name + ">"
	if e.ID != "" {
		label += " #" + e.ID
	}
	var origin = Origin{}
	for _, d := range decls {
		switch d.prop {
		case "transform-origin":
			o, ok := parseOrigin(d.val)
			if ok {
				o.FillBox = origin.FillBox
				origin = o
			} else {
				a.warn("%s has the transform-origin %q, which is not understood: 0 0 is used", label, d.val)
			}
		case "transform-box":
			origin.FillBox = d.val == "fill-box" || d.val == "stroke-box" || d.val == "border-box" || d.val == "content-box"
		}
	}
	for _, sp := range a.specs(decls) {
		if sp.name == "" {
			continue
		}
		frames, ok := keyframes[sp.name]
		if !ok || len(frames) == 0 {
			a.warn("%s uses the animation %q, which no @keyframes defines", label, sp.name)
			continue
		}
		if sp.dur <= 0 {
			a.warn("%s: the animation %q has no duration: it does not run", label, sp.name)
			continue
		}
		if sp.paused {
			a.warn("%s: the animation %q is paused: left out", label, sp.name)
			continue
		}
		props := map[string]bool{}
		var order []string
		for _, f := range frames {
			for _, d := range f.decls {
				if animDecl(d.prop) || props[d.prop] {
					continue
				}
				props[d.prop] = true
				order = append(order, d.prop)
			}
		}
		for _, prop := range order {
			if !cssAnimatable[prop] {
				a.warn("%s: the property %s of %q is not animated by the export", label, prop, sp.name)
				continue
			}
			if len(a.Tracks) >= a.lim.MaxTracks {
				a.warn("more than %d animations: the rest are left out", a.lim.MaxTracks)
				return
			}
			tr, ok := a.cssTrack(ei, label, sp, prop, frames, origin)
			if ok {
				a.Tracks = append(a.Tracks, tr)
			}
		}
	}
}

func (a *Animations) cssTrack(ei int, label string, sp animSpec, prop string, frames []cssFrame, origin Origin) (Track, bool) {
	e := a.Elements[ei]
	type pt struct {
		at   float64
		val  string
		ease *Easing
	}
	var pts []pt
	for _, f := range frames {
		var val string
		found := false
		var ease *Easing
		for _, d := range f.decls {
			if d.prop == prop {
				val, found = d.val, true
			}
			if d.prop == "animation-timing-function" {
				if t, ok := parseTiming(d.val); ok {
					ease = &t
				}
			}
		}
		if found {
			pts = append(pts, pt{f.at, val, ease})
		}
	}
	if len(pts) == 0 {
		return Track{}, false
	}
	base := ""
	if prop == "transform" {
		base = "none"
	} else if v, ok := a.baseValue(ei, prop); ok {
		base = v.String()
	}
	if pts[0].at > 0 {
		if base == "" {
			a.warn("%s: %s has no 0%% keyframe and no value of its own to start from: left out", label, prop)
			return Track{}, false
		}
		pts = append([]pt{{0, base, nil}}, pts...)
	}
	if pts[len(pts)-1].at < 1 {
		if base == "" {
			a.warn("%s: %s has no 100%% keyframe and no value of its own to end on: left out", label, prop)
			return Track{}, false
		}
		pts = append(pts, pt{1, base, nil})
	}
	tr := Track{
		Element: ei, ID: e.ID, Source: CSS, Name: prop, Origin: origin,
		Begin: sp.delay, Dur: sp.dur, Repeat: sp.count, End: math.Inf(1),
		Direction: sp.dir, Freeze: sp.fill == "forwards" || sp.fill == "both", Backwards: sp.fill == "backwards" || sp.fill == "both",
	}
	if sp.count == 0 {
		return Track{}, false
	}
	raws := make([]string, len(pts))
	for i, p := range pts {
		raws[i] = p.val
		tr.KeyTimes = append(tr.KeyTimes, p.at)
		if i < len(pts)-1 {
			ease := sp.timing
			if p.ease != nil {
				ease = *p.ease
			}
			tr.Easing = append(tr.Easing, ease)
		}
	}
	switch prop {
	case "transform":
		norm, exact := harmonizeTransforms(raws)
		if norm == nil {
			a.warn("%s: a transform of %q is not understood (a percentage, 3D or var()): left out", label, sp.name)
			return Track{}, false
		}
		if !exact {
			a.warn("%s: the transforms of %q list different functions: they interpolate as matrices", label, sp.name)
		}
		raws = norm
	case "d":
		for i, r := range raws {
			rt := strings.TrimSpace(r)
			if in, ok := funcArgs(strings.ToLower(rt), "path"); ok {
				raws[i] = strings.Trim(strings.TrimSpace(in), `"'`)
			}
		}
	case "visibility":
		tr.Calc = Discrete
	}
	for _, r := range raws {
		if strings.Contains(r, "var(") || strings.Contains(r, "calc(") {
			a.warn("%s: %s of %q uses var() or calc(): left out", label, prop, sp.name)
			return Track{}, false
		}
		tr.Values = append(tr.Values, ParseValue(r))
	}
	if tr.Calc == Discrete {
		// every interval holds its first value until the next keyframe
		tr.Easing = nil
	}
	return tr, true
}

// parseOrigin reads a transform-origin of one or two components.
func parseOrigin(s string) (Origin, bool) {
	f := strings.Fields(strings.ToLower(s))
	if len(f) == 0 || len(f) > 2 {
		return Origin{}, false
	}
	if len(f) == 1 {
		f = append(f, "center")
		if f[0] == "top" || f[0] == "bottom" {
			f = []string{"center", f[0]}
		}
	}
	var o Origin
	comp := func(t string, horiz bool) (v float64, pct, ok bool) {
		switch t {
		case "left":
			return 0, true, horiz
		case "right":
			return 100, true, horiz
		case "top":
			return 0, true, !horiz
		case "bottom":
			return 100, true, !horiz
		case "center":
			return 50, true, true
		}
		if strings.HasSuffix(t, "%") {
			x, err := strconv.ParseFloat(t[:len(t)-1], 64)
			return x, true, err == nil
		}
		x, err := strconv.ParseFloat(strings.TrimSuffix(t, "px"), 64)
		return x, false, err == nil
	}
	var ok1, ok2 bool
	o.X, o.XPct, ok1 = comp(f[0], true)
	o.Y, o.YPct, ok2 = comp(f[1], false)
	if !ok1 || !ok2 {
		// "top left": the keywords may come in either order
		o.X, o.XPct, ok1 = comp(f[1], true)
		o.Y, o.YPct, ok2 = comp(f[0], false)
	}
	return o, ok1 && ok2
}

// harmonizeTransforms writes each CSS transform as a canonical list of
// functions with unitless numbers (angles in degrees, lengths in px) and
// gives the values the same function list, as CSS Transforms 1 requires for a
// function-wise interpolation: "none" becomes the identity of the others'
// functions. When the lists differ, every value becomes a matrix. exact is
// false in that case. It returns nil when a value is not understood.
func harmonizeTransforms(raws []string) (out []string, exact bool) {
	type fn struct {
		name string
		args []float64
	}
	parse := func(s string) ([]fn, bool) {
		s = strings.TrimSpace(s)
		if s == "none" || s == "" {
			return nil, true
		}
		var fns []fn
		for _, part := range strings.SplitAfter(s, ")") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			open := strings.IndexByte(part, '(')
			if open <= 0 || !strings.HasSuffix(part, ")") {
				return nil, false
			}
			name := strings.ToLower(strings.TrimSpace(part[:open]))
			angle := strings.HasPrefix(name, "rotate") || strings.HasPrefix(name, "skew")
			var args []float64
			for _, t := range strings.FieldsFunc(part[open+1:len(part)-1], func(r rune) bool { return r == ',' || r == ' ' }) {
				v, ok := cssNumber(t, angle)
				if !ok {
					return nil, false
				}
				args = append(args, v)
			}
			// a single canonical arity per function, so that lists match
			switch name {
			case "translate":
				for len(args) < 2 {
					args = append(args, 0)
				}
			case "scale":
				if len(args) == 1 {
					args = append(args, args[0])
				}
			case "translatex":
				name, args = "translate", []float64{args[0], 0}
			case "translatey":
				name, args = "translate", []float64{0, args[0]}
			case "scalex":
				name, args = "scale", []float64{args[0], 1}
			case "scaley":
				name, args = "scale", []float64{1, args[0]}
			case "rotate":
				if len(args) != 1 {
					return nil, false
				}
			case "skewx", "skewy", "matrix":
			default:
				return nil, false
			}
			fns = append(fns, fn{name, args})
		}
		return fns, true
	}
	lists := make([][]fn, len(raws))
	for i, r := range raws {
		l, ok := parse(r)
		if !ok {
			return nil, false
		}
		lists[i] = l
	}
	var tmpl []fn
	for _, l := range lists {
		if len(l) > 0 {
			tmpl = l
			break
		}
	}
	same := true
	for _, l := range lists {
		if len(l) == 0 {
			continue
		}
		if len(l) != len(tmpl) {
			same = false
			break
		}
		for i := range l {
			if l[i].name != tmpl[i].name || len(l[i].args) != len(tmpl[i].args) {
				same = false
			}
		}
	}
	for _, l := range lists {
		_ = l
	}
	out = make([]string, len(raws))
	if !same {
		for i, r := range raws {
			m, ok := ParseTransform(r)
			if !ok {
				return nil, false
			}
			out[i] = m.String()
		}
		return out, false
	}
	ident := func(f fn) []float64 {
		v := make([]float64, len(f.args))
		switch f.name {
		case "scale":
			for i := range v {
				v[i] = 1
			}
		case "matrix":
			v = []float64{1, 0, 0, 1, 0, 0}
		}
		return v
	}
	for i, l := range lists {
		var b strings.Builder
		if len(l) == 0 {
			l = make([]fn, len(tmpl))
			for k, f := range tmpl {
				l[k] = fn{f.name, ident(f)}
			}
		}
		for k, f := range l {
			if k > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(f.name + "(")
			for j, v := range f.args {
				if j > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(strconv.FormatFloat(v, 'g', 8, 64))
			}
			b.WriteByte(')')
		}
		out[i] = b.String()
	}
	return out, true
}
