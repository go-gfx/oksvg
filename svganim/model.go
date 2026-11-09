// Package svganim is the animation model of an SVG document: the SMIL
// animations (animate, animateTransform, animateMotion, set, animateColor)
// and the CSS animations (@keyframes with the animation properties) it
// carries, as timelines per element, with an evaluator that gives the state
// of every animated element at a time t.
//
// It is the model an exporter needs to carry an animated drawing into a format
// that animates its own way (a PowerPoint or OpenDocument slide), or to draw
// frames of it where that format cannot say what the drawing does. It reads
// what SVG Animations Level 2 (SMIL Animation, W3C) and CSS Animations Level 1
// define, and says in Animations.Warnings what it did not understand and
// left out, never silently.
//
// An element is named by its key: its position among the document's elements
// in document order, from 0 (the root svg), the key oksvg gives the shapes it
// draws (oksvg.SvgPath.Keys).
//
// Every read is bounded (Limits): a hostile document costs a refusal, not
// memory or time.
package svganim

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

// Limits bound what a document may ask. The zero value is DefaultLimits.
type Limits struct {
	MaxInput     int     // bytes of SVG read
	MaxElements  int     // elements in the document
	MaxTracks    int     // animations kept
	MaxKeyframes int     // keyframes or values of one animation
	MaxDuration  float64 // seconds: the timeline Period may span
	MaxFrames    int     // frames Frames may ask for
}

// DefaultLimits are the limits applied when none are given.
var DefaultLimits = Limits{MaxInput: 8 << 20, MaxElements: 50000, MaxTracks: 2000, MaxKeyframes: 256, MaxDuration: 60, MaxFrames: 240}

func (l Limits) norm() Limits {
	d := DefaultLimits
	if l.MaxInput > 0 {
		d.MaxInput = l.MaxInput
	}
	if l.MaxElements > 0 {
		d.MaxElements = l.MaxElements
	}
	if l.MaxTracks > 0 {
		d.MaxTracks = l.MaxTracks
	}
	if l.MaxKeyframes > 0 {
		d.MaxKeyframes = l.MaxKeyframes
	}
	if l.MaxDuration > 0 {
		d.MaxDuration = l.MaxDuration
	}
	if l.MaxFrames > 0 {
		d.MaxFrames = l.MaxFrames
	}
	return d
}

// Calc is how a track moves between its values (calcMode).
type Calc int

// The calc modes. Paced is SMIL's: constant speed along the values' distances.
const (
	Linear Calc = iota
	Discrete
	Paced
)

// Easing shapes the progress inside one interval between two keyframes: a
// cubic Bézier (keySplines, CSS cubic-bezier(), and the keywords), or steps.
type Easing struct {
	Spline    [4]float64 // x1 y1 x2 y2; the zero value, with Steps 0, is linear
	Steps     int        // when > 0: that many equal jumps (CSS steps())
	StepStart bool       // jump at the start of each step, not the end
	set       bool
}

// IsLinear reports the identity easing.
func (e Easing) IsLinear() bool {
	return e.Steps == 0 && (!e.set || e.Spline == [4]float64{0, 0, 1, 1})
}

// Source says which language an animation was written in.
type Source int

// The sources.
const (
	SMIL Source = iota
	CSS
)

func (s Source) String() string {
	if s == CSS {
		return "css"
	}
	return "smil"
}

// Direction is CSS animation-direction.
type Direction int

// The directions.
const (
	Normal Direction = iota
	Reverse
	Alternate
	AlternateReverse
)

// Rotation is how an animateMotion turns the element: not at all, to follow
// the path (auto, auto-reverse), or by a fixed angle.
type Rotation int

// The rotation modes.
const (
	RotNone Rotation = iota
	RotAuto
	RotAutoReverse
	RotAngle
)

// Motion is an animateMotion's path.
type Motion struct {
	Segs      []Seg
	Walk      *Walk
	Rotate    Rotation
	Angle     float64   // RotAngle, in degrees
	KeyPoints []float64 // the fraction of the path's length at each key time, when given
}

// Origin is a CSS transform-origin and transform-box.
type Origin struct {
	X, Y       float64
	XPct, YPct bool // the coordinate is a percentage of the box
	FillBox    bool // transform-box: fill-box (the element's bounding box), else view-box
}

// Track is one animation of one property of one element.
type Track struct {
	Element int    // the target's key
	ID      string // the target's id attribute, "" when it has none
	Source  Source
	// Name is the animated attribute or CSS property; "transform" for an
	// animateTransform and for the CSS transform, "motion" for an animateMotion.
	Name string
	// Type is an animateTransform's type: translate, scale, rotate, skewX or
	// skewY, its values padded to the full arity (rotate: angle cx cy). Empty
	// for the CSS transform, whose values are transform lists.
	Type string

	Values   []Value
	KeyTimes []float64 // 0..1, one per value
	Easing   []Easing  // one per interval; nil is linear
	Calc     Calc
	Motion   *Motion
	Origin   Origin

	Begin     float64 // seconds from the document start; may be negative (CSS)
	Dur       float64 // one iteration, seconds; math.Inf(1) for a set with no dur
	Repeat    float64 // iteration count; math.Inf(1) is indefinite
	RepeatDur float64 // 0: not set; math.Inf(1) indefinite
	End       float64 // the end of the active duration, as a time; math.Inf(1) none
	Freeze    bool    // fill="freeze" / animation-fill-mode forwards or both
	Backwards bool    // CSS animation-fill-mode backwards or both
	Sum       bool    // additive="sum"
	Direction Direction

	animID string // the id of the animation element, for syncbases
}

// Infinite reports a track that never stops.
func (t *Track) Infinite() bool {
	return math.IsInf(t.ActiveDur(), 1)
}

// ActiveDur is how long the track runs (SMIL Animation, 3.6.5).
func (t *Track) ActiveDur() float64 {
	ad := math.Inf(1)
	if t.Dur > 0 && !math.IsInf(t.Dur, 1) {
		rep := t.Repeat
		if rep <= 0 {
			rep = 1
		}
		ad = t.Dur * rep
	} else if !math.IsInf(t.Dur, 1) {
		return 0
	}
	if t.RepeatDur > 0 {
		if math.IsInf(t.Dur, 1) || math.IsInf(ad, 1) || t.Repeat == 0 {
			ad = t.RepeatDur
		} else {
			ad = math.Min(ad, t.RepeatDur)
		}
	}
	if !math.IsInf(t.End, 1) {
		ad = math.Min(ad, t.End-t.Begin)
	}
	return math.Max(ad, 0)
}

// ElementInfo is what the model keeps of each element of the document.
type ElementInfo struct {
	Name   string
	Parent int // the key of the parent, -1 for the root
	ID     string
	Attrs  map[string]string
}

// Animations is the animation model of one document.
type Animations struct {
	Elements []ElementInfo
	Tracks   []Track
	// Warnings say, one line each, what was found and not carried: an
	// animation that starts on an event, a property or function with no
	// model here, a limit that cut the document short.
	Warnings []string

	sync []syncRef
	own  []Matrix // each element's own static transform
	ctm  []Matrix // each element's ancestors' static transforms, the element's own not included
	lim  Limits
}

// ErrTooLarge is the refusal of a document past a limit that cannot be
// carried on without.
var ErrTooLarge = errors.New("svganim: the document is past a limit")

// Animated reports that the document animates anything.
func (a *Animations) Animated() bool { return len(a.Tracks) > 0 }

// Targets is the keys of the animated elements, in document order.
func (a *Animations) Targets() []int {
	seen := map[int]bool{}
	var out []int
	for _, t := range a.Tracks {
		if !seen[t.Element] {
			seen[t.Element] = true
			out = append(out, t.Element)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// ParentCTM is the product of the static transforms of the element's
// ancestors: the matrix from the element's parent's user space to the
// document's root user space.
func (a *Animations) ParentCTM(e int) Matrix {
	if e < 0 || e >= len(a.ctm) {
		return Identity
	}
	return a.ctm[e]
}

// OwnTransform is the element's own static transform attribute.
func (a *Animations) OwnTransform(e int) Matrix {
	if e < 0 || e >= len(a.own) {
		return Identity
	}
	return a.own[e]
}

// Ancestor reports whether anc is an ancestor of e (or e itself).
func (a *Animations) Ancestor(anc, e int) bool {
	for n := 0; e >= 0 && e < len(a.Elements) && n <= len(a.Elements); n++ {
		if e == anc {
			return true
		}
		e = a.Elements[e].Parent
	}
	return false
}

func (a *Animations) warn(format string, args ...any) {
	if len(a.Warnings) < 64 {
		a.Warnings = append(a.Warnings, fmt.Sprintf(format, args...))
	}
}

// animTags are the SMIL animation elements.
var animTags = map[string]bool{"animate": true, "animateTransform": true, "animateMotion": true, "set": true, "animateColor": true}

// IsAnimationElement reports the SMIL elements an exporter leaves out of the
// static drawing.
func IsAnimationElement(name string) bool { return animTags[name] || name == "mpath" }

// Parse reads the animations of an SVG document. It fails only for a document
// that is not XML or is past a limit; what it does not understand it leaves
// out and notes in Warnings.
func Parse(svg []byte, lim Limits) (*Animations, error) {
	lim = lim.norm()
	if len(svg) > lim.MaxInput {
		return nil, fmt.Errorf("%w: %d bytes of SVG, at most %d", ErrTooLarge, len(svg), lim.MaxInput)
	}
	a := &Animations{lim: lim}
	dec := xml.NewDecoder(bytes.NewReader(svg))
	dec.Strict = true
	var stack []int
	var styleText strings.Builder
	var inStyle int
	type rawAnim struct {
		elem   int
		parent int
		name   string
		attrs  map[string]string
	}
	var raws []rawAnim
	for {
		tok, err := dec.RawToken()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(a.Elements) >= lim.MaxElements {
				return nil, fmt.Errorf("%w: more than %d elements", ErrTooLarge, lim.MaxElements)
			}
			attrs := make(map[string]string, len(t.Attr))
			for _, at := range t.Attr {
				attrs[at.Name.Local] = at.Value // xlink:href and href share a key
			}
			parent := -1
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			key := len(a.Elements)
			a.Elements = append(a.Elements, ElementInfo{Name: t.Name.Local, Parent: parent, ID: attrs["id"], Attrs: attrs})
			stack = append(stack, key)
			if animTags[t.Name.Local] {
				raws = append(raws, rawAnim{elem: key, parent: parent, name: t.Name.Local, attrs: attrs})
			}
			if t.Name.Local == "style" {
				inStyle++
			}
		case xml.EndElement:
			if len(stack) > 0 {
				if a.Elements[stack[len(stack)-1]].Name == "style" {
					inStyle--
				}
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if inStyle > 0 && styleText.Len() < lim.MaxInput {
				styleText.Write(t)
			}
		}
	}
	if len(a.Elements) == 0 {
		return nil, errors.New("svganim: not an SVG document")
	}
	a.computeTransforms()
	ids := map[string]int{}
	for i, e := range a.Elements {
		if e.ID != "" {
			if _, dup := ids[e.ID]; !dup {
				ids[e.ID] = i
			}
		}
	}
	for _, r := range raws {
		if len(a.Tracks) >= lim.MaxTracks {
			a.warn("more than %d animations: the rest are left out", lim.MaxTracks)
			break
		}
		target := r.parent
		if h := r.attrs["href"]; strings.HasPrefix(h, "#") {
			if k, ok := ids[h[1:]]; ok {
				target = k
			} else {
				a.warn("<%s> targets #%s, which is not in the document", r.name, h[1:])
				continue
			}
		}
		if target < 0 {
			continue
		}
		mpath := ""
		if r.name == "animateMotion" {
			mpath = raws2motion(a, r.elem, ids)
		}
		a.parseSMIL(r.elem, target, r.name, r.attrs, ids, mpath)
	}
	a.resolveSyncbases(ids)
	a.parseCSS(styleText.String())
	return a, nil
}

// raws2motion finds the mpath child of an animateMotion: its href is the
// path element whose d is the motion.
func raws2motion(a *Animations, motionElem int, ids map[string]int) string {
	for i := motionElem + 1; i < len(a.Elements); i++ {
		if !a.Ancestor(motionElem, i) {
			break
		}
		if a.Elements[i].Name == "mpath" {
			if h := a.Elements[i].Attrs["href"]; strings.HasPrefix(h, "#") {
				if k, ok := ids[h[1:]]; ok {
					return a.Elements[k].Attrs["d"]
				}
			}
		}
	}
	return ""
}

// computeTransforms fills each element's own transform and its ancestors'.
func (a *Animations) computeTransforms() {
	a.own = make([]Matrix, len(a.Elements))
	a.ctm = make([]Matrix, len(a.Elements))
	for i, e := range a.Elements {
		a.own[i] = Identity
		if s, ok := e.Attrs["transform"]; ok {
			if m, ok := ParseTransform(s); ok {
				a.own[i] = m
			}
		}
		if e.Parent >= 0 {
			a.ctm[i] = a.ctm[e.Parent].Mul(a.own[e.Parent])
		} else {
			a.ctm[i] = Identity
		}
	}
}
