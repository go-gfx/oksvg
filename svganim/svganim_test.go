package svganim

import (
	"bytes"
	"image"
	"image/gif"
	"math"
	"strings"
	"testing"

	"github.com/go-gfx/oksvg"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func mustParse(t *testing.T, svg string) *Animations {
	t.Helper()
	a, err := Parse([]byte(svg), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

const spinner = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100" width="100" height="100">
<rect id="bg" width="100" height="100" fill="#eee"/>
<g id="arm"><rect x="48" y="10" width="4" height="40" fill="#c00"/>
<animateTransform attributeName="transform" type="rotate" from="0 50 50" to="360 50 50" dur="2s" repeatCount="indefinite"/></g>
<circle id="dot" cx="20" cy="20" r="5" fill="red">
<animate attributeName="opacity" values="1;0;1" dur="1s" repeatCount="3"/>
<animate attributeName="fill" from="red" to="blue" dur="2s" begin="0.5s" fill="freeze"/>
<set attributeName="r" to="9" begin="1s" dur="1s"/>
</circle>
<circle id="ball" r="4" fill="green"><animateMotion path="M10,10 L90,10" dur="4s" rotate="auto"/></circle>
</svg>`

func TestParseSMIL(t *testing.T) {
	a := mustParse(t, spinner)
	if len(a.Tracks) != 5 {
		t.Fatalf("tracks = %d, want 5: %+v", len(a.Tracks), a.Warnings)
	}
	rot := a.Tracks[0]
	if rot.Name != "transform" || rot.Type != "rotate" || rot.ID != "arm" || !rot.Infinite() || rot.Dur != 2 {
		t.Fatalf("rotate track = %+v", rot)
	}
	if got := rot.Values[1].Nums; len(got) != 3 || got[0] != 360 || got[1] != 50 {
		t.Fatalf("rotate values = %v", got)
	}
	op := a.Tracks[1]
	if op.Name != "opacity" || op.Repeat != 3 || len(op.Values) != 3 || op.ActiveDur() != 3 {
		t.Fatalf("opacity track = %+v", op)
	}
	fill := a.Tracks[2]
	if !fill.Freeze || fill.Begin != 0.5 || fill.Values[0].Color == nil {
		t.Fatalf("fill track = %+v", fill)
	}
	set := a.Tracks[3]
	if set.Calc != Discrete || set.Begin != 1 || set.ActiveDur() != 1 {
		t.Fatalf("set track = %+v", set)
	}
	m := a.Tracks[4]
	if m.Name != "motion" || m.Motion == nil || m.Motion.Rotate != RotAuto || !near(m.Motion.Walk.Length(), 80) {
		t.Fatalf("motion track = %+v", m)
	}
	if got := a.Targets(); len(got) != 3 {
		t.Fatalf("targets = %v", got)
	}
}

func TestEval(t *testing.T) {
	a := mustParse(t, spinner)
	s := a.Eval(0.5, nil)
	arm := s[a.Tracks[0].Element]
	if arm == nil || arm.Transform == nil {
		t.Fatal("no transform for the arm")
	}
	// a quarter turn about (50,50): (50,10) goes to (90,50)
	x, y := arm.Transform.Apply(50, 10)
	if !near(x, 90) || !near(y, 50) {
		t.Fatalf("(50,10) -> (%v,%v)", x, y)
	}
	dot := s[a.Tracks[1].Element]
	if got := dot.Attrs["opacity"].Nums[0]; !near(got, 0) { // values 1;0;1 over 1s: at 0.5s it is 0
		t.Fatalf("opacity = %v", got)
	}
	if c := a.Eval(1, nil)[a.Tracks[2].Element].Attrs["fill"].Color; c == nil || c.R != 191 || c.B != 64 { // 0.5s into 2s: a quarter of the way
		t.Fatalf("fill = %+v", c)
	}
	if _, ok := dot.Attrs["r"]; ok {
		t.Fatal("the set has not begun at 0.5s")
	}
	s = a.Eval(1.5, nil)
	if got := s[a.Tracks[3].Element].Attrs["r"].Nums[0]; got != 9 {
		t.Fatalf("r = %v during the set", got)
	}
	s = a.Eval(2.5, nil)
	if _, ok := s[a.Tracks[3].Element].Attrs["r"]; ok {
		t.Fatal("the set has ended at 2.5s, with no freeze")
	}
	if c := s[a.Tracks[2].Element].Attrs["fill"].Color; c == nil || c.B != 255 {
		t.Fatalf("fill is not frozen on blue: %+v", c)
	}
	ball := s[a.Tracks[4].Element]
	// 2.5 s of 4 s along 80 units from (10,10): x = 10 + 50, turned 0 degrees
	x, y = ball.Transform.Apply(0, 0)
	if !near(x, 60) || !near(y, 10) {
		t.Fatalf("ball at (%v,%v)", x, y)
	}
}

func TestRepeatAndDirection(t *testing.T) {
	a := mustParse(t, `<svg viewBox="0 0 10 10"><rect id="r" width="1" height="1">
<animate attributeName="x" from="0" to="10" dur="2s" repeatCount="1.5"/></rect></svg>`)
	for _, c := range []struct{ at, want float64 }{{0, 0}, {1, 5}, {2, 0}, {2.5, 2.5}} {
		got := a.Eval(c.at, nil)[1]
		if got == nil || !near(got.Attrs["x"].Nums[0], c.want) {
			t.Fatalf("x at %v = %+v, want %v", c.at, got, c.want)
		}
	}
	if s := a.Eval(3.5, nil); len(s) != 0 {
		t.Fatalf("ended at 3 s but still animates at 3.5 s: %+v", s)
	}
}

func TestSplineKeyTimesDiscrete(t *testing.T) {
	a := mustParse(t, `<svg viewBox="0 0 10 10"><rect width="1" height="1">
<animate attributeName="x" values="0;10;20" keyTimes="0;0.25;1" dur="4s"/>
<animate attributeName="y" values="0;8" calcMode="spline" keySplines="0.5 0 0.5 1" dur="4s"/>
<animate attributeName="width" values="1;2;3" calcMode="discrete" dur="3s"/></rect></svg>`)
	s := a.Eval(1, nil)[1] // 1s of 4: keyTime .25 -> x = 10
	if !near(s.Attrs["x"].Nums[0], 10) {
		t.Fatalf("x = %v", s.Attrs["x"])
	}
	if got := a.Eval(2.5, nil)[1].Attrs["x"].Nums[0]; !near(got, 10+10*(0.625-0.25)/0.75) {
		t.Fatalf("x at 2.5s = %v", got)
	}
	if got := a.Eval(2, nil)[1].Attrs["y"].Nums[0]; !near(got, 4) { // the symmetric spline passes (0.5, 0.5)
		t.Fatalf("y = %v", got)
	}
	if got := a.Eval(1.5, nil)[1].Attrs["width"].Nums[0]; got != 2 {
		t.Fatalf("discrete width = %v", got)
	}
}

func TestPathMorphIsInterpolated(t *testing.T) {
	a := mustParse(t, `<svg viewBox="0 0 10 10"><path d="M0 0L10 0L10 10Z">
<animate attributeName="d" values="M0 0L10 0L10 10Z;M0 0L20 0L20 20Z" dur="2s"/></path></svg>`)
	got := a.Eval(1, nil)[1].Attrs["d"].String()
	if !strings.Contains(got, "15") {
		t.Fatalf("d = %q", got)
	}
}

func TestSyncbaseAndEvents(t *testing.T) {
	a := mustParse(t, `<svg viewBox="0 0 10 10"><rect id="r" width="1" height="1">
<animate id="a1" attributeName="x" from="0" to="1" begin="1s" dur="2s"/>
<animate attributeName="y" from="0" to="1" begin="a1.end+0.5s" dur="1s"/>
<animate attributeName="width" from="1" to="2" begin="click" dur="1s"/>
<animate attributeName="height" from="1" to="2" begin="indefinite" dur="1s"/></rect></svg>`)
	if len(a.Tracks) != 2 || a.Tracks[1].Begin != 3.5 {
		t.Fatalf("tracks = %+v", a.Tracks)
	}
	if len(a.Warnings) != 2 || !strings.Contains(a.Warnings[0], "click") {
		t.Fatalf("warnings = %v", a.Warnings)
	}
}

const cssSpin = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
<style>
@keyframes spin { from { transform: rotate(0deg) } to { transform: rotate(360deg) } }
@keyframes pulse { 0% { opacity: 1 } 50% { opacity: .2; animation-timing-function: ease-in } 100% { opacity: 1 } }
.s { animation: spin 2s linear infinite; transform-origin: center; transform-box: fill-box }
#p { animation: pulse 1s 0.5s 3 alternate both }
</style>
<rect class="s" x="20" y="20" width="40" height="20" fill="blue"/>
<circle id="p" cx="80" cy="80" r="10" fill="red"/>
<circle class="s ghost" cx="50" cy="50" r="2"/>
</svg>`

func TestParseCSS(t *testing.T) {
	a := mustParse(t, cssSpin)
	if len(a.Tracks) != 3 {
		t.Fatalf("tracks = %d: %v", len(a.Tracks), a.Warnings)
	}
	spin := a.Tracks[0]
	if spin.Source != CSS || spin.Name != "transform" || spin.Dur != 2 || !spin.Infinite() || !spin.Origin.FillBox || !spin.Origin.XPct || spin.Origin.X != 50 {
		t.Fatalf("spin = %+v", spin)
	}
	if !spin.Easing[0].IsLinear() {
		t.Fatal("linear timing expected")
	}
	pulse := a.Tracks[1]
	if pulse.Name != "opacity" || pulse.Begin != 0.5 || pulse.Repeat != 3 || pulse.Direction != Alternate || !pulse.Freeze || !pulse.Backwards {
		t.Fatalf("pulse = %+v", pulse)
	}
	if len(pulse.Easing) != 2 || pulse.Easing[0].IsLinear() || pulse.Easing[1].IsLinear() {
		t.Fatalf("pulse easing = %+v", pulse.Easing)
	}
	// the rotation of a fill-box about its centre
	bbox := func(e int) (x, y, w, h float64, ok bool) { return 20, 20, 40, 20, true }
	s := a.Eval(0.5, bbox)[a.Tracks[0].Element]
	x, y := s.Transform.Apply(60, 30) // the right-hand middle goes, a quarter turn about (40,30), to (40,50)
	if !near(x, 40) || !near(y, 50) {
		t.Fatalf("(60,30) -> (%v,%v)", x, y)
	}
	// backwards fill: before the delay the first keyframe holds
	if got := a.Eval(0, nil)[a.Tracks[1].Element].Attrs["opacity"].Nums[0]; got != 1 {
		t.Fatalf("opacity before the delay = %v", got)
	}
}

func TestCSSWarnings(t *testing.T) {
	a := mustParse(t, `<svg viewBox="0 0 10 10"><style>
@keyframes k { to { filter: blur(2px); stroke-width: 5 } }
.a { animation: k 1s } .b > .c { animation: k 1s } .d { animation: missing 1s }
</style><rect class="a" width="1" height="1"/><rect class="d" width="1" height="1"/></svg>`)
	if len(a.Tracks) != 1 || a.Tracks[0].Name != "stroke-width" {
		t.Fatalf("tracks = %+v", a.Tracks)
	}
	w := strings.Join(a.Warnings, "|")
	for _, want := range []string{"filter", ".b > .c", "missing"} {
		if !strings.Contains(w, want) {
			t.Errorf("no warning about %q in %q", want, w)
		}
	}
}

func TestApplyKeepsKeysAndRendersTheState(t *testing.T) {
	a := mustParse(t, spinner)
	out, err := a.Apply([]byte(spinner), 0.5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("animate")) {
		t.Fatalf("animation elements left: %s", out)
	}
	b, err := Parse(out, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Elements) != len(a.Elements) {
		t.Fatalf("elements %d, want %d", len(b.Elements), len(a.Elements))
	}
	if b.Tracks != nil {
		t.Fatal("the result still animates")
	}
	// the shapes oksvg draws carry the keys of the document
	icon, err := oksvg.ReadIconStream(bytes.NewReader(out), oksvg.IgnoreErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	shapes, _ := icon.Shapes()
	arm := a.Tracks[0].Element
	x, y, w, h, ok := ElementBounds(shapes, arm)
	if !ok {
		t.Fatalf("no shape with the key %d", arm)
	}
	// the arm turned a quarter about (50,50): it lies horizontally, to the right
	if !(w > 30 && h < 8 && x > 48 && math.Abs(y+h/2-50) < 3) {
		t.Fatalf("arm at %v %v %v %v", x, y, w, h)
	}
}

func TestApplyAnOpaqueKeyframesSheetRenders(t *testing.T) {
	a := mustParse(t, cssSpin)
	out, err := a.Apply([]byte(cssSpin), 0.5, func(e int) (x, y, w, h float64, ok bool) { return 20, 20, 40, 20, true })
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("keyframes")) {
		t.Fatalf("keyframes left: %s", out)
	}
	if _, err := oksvg.ReadIconStream(bytes.NewReader(out), oksvg.IgnoreErrorMode); err != nil {
		t.Fatal(err)
	}
}

func TestSpanAndFrames(t *testing.T) {
	a := mustParse(t, spinner)
	s := a.Span()
	if !s.Loops || s.End != 4 || s.Delay { // the 2 s rotation repeats; the 4 s motion ends at 4
		t.Fatalf("span = %+v", s)
	}
	ts, step := a.FrameTimes(s, 10)
	if len(ts) != 40 || !near(step, 0.1) {
		t.Fatalf("frames = %d, step %v", len(ts), step)
	}
	b := mustParse(t, `<svg viewBox="0 0 10 10"><rect id="r" width="1" height="1">
<animate attributeName="x" from="0" to="1" dur="3s" repeatCount="indefinite"/>
<animate attributeName="y" from="0" to="1" dur="2s" repeatCount="indefinite" begin="1s"/></rect></svg>`)
	if s := b.Span(); s.End != 7 || !s.Delay { // lcm(3,2) = 6, after the delay of 1
		t.Fatalf("span = %+v", s)
	}
	c := mustParse(t, `<svg viewBox="0 0 10 10"><rect width="1" height="1"><animate attributeName="x" from="0" to="1" dur="50s" repeatCount="indefinite"/></rect></svg>`)
	ts, _ = c.FrameTimes(c.Span(), 30)
	if len(ts) != DefaultLimits.MaxFrames {
		t.Fatalf("frames = %d, want the limit %d", len(ts), DefaultLimits.MaxFrames)
	}
}

func TestGIFAnimatesTheSpinner(t *testing.T) {
	a := mustParse(t, spinner)
	s := a.Span()
	ts, step := a.FrameTimes(s, 8)
	var frames []*image.RGBA
	for _, tm := range ts {
		svg, err := a.Apply([]byte(spinner), tm, nil)
		if err != nil {
			t.Fatal(err)
		}
		img, err := Rasterize(svg, 64, 64)
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, img)
	}
	data, err := GIF(frames, int(step*1000), s.Loops)
	if err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 32 || g.LoopCount != 0 || g.Delay[0] != 13 { // 125 ms rounds to 13/100 s
		t.Fatalf("gif: %d frames, loop %d, delay %d", len(g.Image), g.LoopCount, g.Delay[0])
	}
	if bytes.Equal(g.Image[0].Pix, g.Image[4].Pix) {
		t.Fatal("the first and the quarter-turn frames are the same")
	}
	once, err := GIF(frames[:2], 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := gif.DecodeAll(bytes.NewReader(once)); g.LoopCount != -1 {
		t.Fatalf("a GIF that plays once has the loop count %d", g.LoopCount)
	}
}

func TestLimits(t *testing.T) {
	if _, err := Parse([]byte(spinner), Limits{MaxInput: 10}); err == nil {
		t.Fatal("an input past MaxInput must be refused")
	}
	if _, err := Parse([]byte(spinner), Limits{MaxElements: 3}); err == nil {
		t.Fatal("more elements than MaxElements must be refused")
	}
	var b strings.Builder
	b.WriteString(`<svg viewBox="0 0 10 10"><rect id="r" width="1" height="1">`)
	for i := 0; i < 50; i++ {
		b.WriteString(`<animate attributeName="x" from="0" to="1" dur="1s"/>`)
	}
	b.WriteString(`</rect></svg>`)
	a, err := Parse([]byte(b.String()), Limits{MaxTracks: 10})
	if err != nil || len(a.Tracks) != 10 || len(a.Warnings) == 0 {
		t.Fatalf("tracks %d, warnings %v, err %v", len(a.Tracks), a.Warnings, err)
	}
	var v strings.Builder
	for i := 0; i < 300; i++ {
		v.WriteString("1;")
	}
	k := mustParse(t, `<svg viewBox="0 0 10 10"><rect width="1" height="1"><animate attributeName="x" values="`+v.String()+`" dur="1s"/></rect></svg>`)
	if len(k.Tracks) != 0 || len(k.Warnings) == 0 {
		t.Fatalf("300 keyframes: %d tracks, %v", len(k.Tracks), k.Warnings)
	}
}

func TestValueAndTransformParsing(t *testing.T) {
	m, ok := ParseTransform("translate(10 20) rotate(90) scale(2)")
	if !ok {
		t.Fatal("not parsed")
	}
	x, y := m.Apply(1, 0) // scale 2 -> (2,0); rotate 90 -> (0,2); translate -> (10,22)
	if !near(x, 10) || !near(y, 22) {
		t.Fatalf("(%v,%v)", x, y)
	}
	for _, bad := range []string{"rotate(1deg", "translate(10%)", "perspective(5)", "matrix(1 2 3)"} {
		if _, ok := ParseTransform(bad); ok {
			t.Errorf("%q must not parse", bad)
		}
	}
	if m, ok := ParseTransform("rotate(0.25turn)"); !ok || !near(m[1], 1) {
		t.Fatalf("0.25turn: %v %v", m, ok)
	}
	v, w := ParseValue("10px 20px"), ParseValue("30px 40px")
	if mid, ok := v.Lerp(w, 0.5); !ok || mid.String() != "20px 30px" {
		t.Fatalf("lerp = %v %v", mid, ok)
	}
	if _, ok := ParseValue("10px").Lerp(ParseValue("2em"), 0.5); ok {
		t.Fatal("px and em must not interpolate")
	}
	if c := ParseValue("rgb(10,20,30)").Color; c == nil || c.G != 20 {
		t.Fatalf("color = %v", c)
	}
	if ParseValue("round").Color != nil || ParseValue("none").Color != nil {
		t.Fatal("a word is not a colour")
	}
	if segs, ok := ParsePath("M10 10 h 20 v 20 H 0 Z"); !ok || len(segs) != 5 || segs[3].P[0] != (Point{0, 30}) {
		t.Fatalf("path = %v %v", segs, ok)
	}
	if segs, ok := ParsePath("M0 0 A 10 10 0 0 1 20 0"); !ok || len(segs) != 3 || segs[len(segs)-1].P[2] != (Point{20, 0}) {
		t.Fatalf("arc = %v %v", segs, ok)
	}
	if _, ok := ParsePath("L 1 2"); ok {
		t.Fatal("a path must begin with a move")
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(spinner))
	f.Add([]byte(cssSpin))
	f.Add([]byte(`<svg><animate attributeName="x" values=";;" keyTimes="a;b" dur="-1s" begin="a.begin+x"/></svg>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		a, err := Parse(data, Limits{MaxInput: 1 << 16, MaxElements: 500, MaxTracks: 50, MaxKeyframes: 32})
		if err != nil {
			return
		}
		s := a.Span()
		ts, _ := a.FrameTimes(s, 10)
		for _, tm := range ts[:min(len(ts), 4)] {
			a.Eval(tm, nil)
		}
		if _, err := a.Apply(data, 0.3, nil); err != nil {
			return
		}
	})
}

func FuzzValue(f *testing.F) {
	for _, s := range []string{"10px 20px", "M0 0L10 10Z", "rotate(45deg)", "#fff", "1e3", "--", "hsl(,,)"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v := ParseValue(s)
		_ = v.String()
		v.Lerp(ParseValue(s+" 1"), 0.5)
		ParseTransform(s)
		ParsePath(s)
	})
}
