// Copyright (c) 2026 the go-gfx authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package oksvg

import (
	"image"
	"image/color"
	"math"
	"sort"

	"github.com/srwiley/rasterx"
	"golang.org/x/image/math/fixed"
)

// The icon as vector shapes, for a writer that draws paths itself (a PDF, a
// PowerPoint slide) instead of rasterising: Shapes returns what Draw would
// paint, in the icon's viewBox units, each fill and each stroke a filled
// outline with a solid colour or a gradient, under the clips in force.
//
// SVG 1.1 painting (https://www.w3.org/TR/SVG11/painting.html) is what Draw
// implements, and Shapes follows Draw: a stroke is the outline rasterx's own
// stroker makes of it (width, caps, joins, miter limit, dashes, as Draw
// strokes it), a fill keeps its curves, transforms are applied to the
// points (an affine map keeps a Bézier curve a Bézier curve). Opacities go
// into the colour's alpha.
//
// Some of what Draw paints has no exact form here, and Shapes then says so
// (exact is false), for the caller to rasterise instead:
//   - a gradient whose stops change opacity (a shading has one), that is
//     reflected or repeated (spreadMethod), a linear one under a skew, a
//     radial one whose focus is off its centre or that is rotated or skewed;
//   - a clipPath whose silhouettes mix clip rules or are clipped
//     themselves (the union of clipped pieces is not one clip path).

// PathOp is one step of a Shape's outline: 'M' move to Pts[0], 'L' line to
// Pts[0], 'Q' quadratic curve through Pts[0] to Pts[1], 'C' cubic curve
// through Pts[0] and Pts[1] to Pts[2], 'Z' close.
type PathOp struct {
	Op  byte
	Pts [3][2]float64
}

// Shape is one painted piece of an icon, in viewBox units.
type Shape struct {
	Path    []PathOp
	EvenOdd bool
	// Color is the paint, alpha included, when Gradient is nil.
	Color    color.NRGBA
	Gradient *VectorGradient
	// Clips are the clip paths in force; the shape shows where it is inside
	// every one.
	Clips []ClipPath
}

// ClipPath is one clip path, in viewBox units.
type ClipPath struct {
	Path    []PathOp
	EvenOdd bool
}

// VectorGradient is a gradient in viewBox units. A linear one runs from
// (X1,Y1) at offset 0 to (X2,Y2) at 1; a radial one from its centre (CX,CY)
// at 0 to its ellipse of radii (RX,RY), axis-aligned, at 1. Both pad: the
// first and last stops' colours extend beyond.
type VectorGradient struct {
	Radial         bool
	X1, Y1, X2, Y2 float64
	CX, CY, RX, RY float64
	Stops          []VectorStop
}

// VectorStop is one stop: an offset in [0,1] and a colour, alpha included.
type VectorStop struct {
	Offset float64
	Color  color.NRGBA
}

// strokeUnits is the size, in fixed-point units, the stroker works at: the
// icon is scaled so that its larger viewBox side is this many units before
// stroking, so that 26.6 fixed point keeps sub-1/1000 precision whatever the
// viewBox, then scaled back.
const strokeUnits = 4096

// Shapes returns the icon's drawing as vector shapes, in drawing order, and
// whether they are exactly what Draw paints (see the package comment of
// this file). A shape a clip removes entirely is left out.
func (s *SvgIcon) Shapes() (shapes []Shape, exact bool) {
	exact = true
	k := strokeUnits / math.Max(math.Max(s.ViewBox.W, s.ViewBox.H), 1e-9)
	for i := range s.SVGPaths {
		p := &s.SVGPaths[i]
		clips, ok, gone := p.vectorClips()
		if !ok {
			exact = false
		}
		if gone {
			continue
		}
		m := p.mAdder.M
		if p.fillerColor != nil {
			sh := Shape{Path: transformPath(p.Path, m), EvenOdd: !p.UseNonZeroWinding, Clips: clips}
			if paintOK := sh.setPaint(p.fillerColor, p.FillOpacity, m); !paintOK {
				exact = false
			}
			if len(sh.Path) > 0 {
				shapes = append(shapes, sh)
			}
		}
		if p.linerColor != nil && p.LineWidth > 0 {
			sh := Shape{Path: p.strokeOutline(k), Clips: clips}
			if paintOK := sh.setPaint(p.linerColor, p.LineOpacity, m); !paintOK {
				exact = false
			}
			if len(sh.Path) > 0 {
				shapes = append(shapes, sh)
			}
		}
	}
	return shapes, exact
}

// setPaint gives the shape its colour or gradient, under the element
// transform m. It reports whether the paint is exact.
func (sh *Shape) setPaint(paint interface{}, opacity float64, m rasterx.Matrix2D) bool {
	switch c := paint.(type) {
	case color.Color:
		sh.Color = rasterx.ApplyOpacity(c, opacity)
		return true
	case rasterx.Gradient:
		g, ok := vectorGradient(c, opacity, m, pathBounds(sh.Path, m))
		if g == nil {
			// no stops: as Draw, which paints black (one stop: its colour)
			sh.Color = rasterx.ApplyOpacity(color.Black, opacity)
			return ok
		}
		if len(g.Stops) == 1 {
			sh.Color = g.Stops[0].Color
			return ok
		}
		sh.Gradient = g
		return ok
	}
	return false
}

// vectorGradient maps a gradient to viewBox units. box is the bounding box,
// in the element's user space, of the shape it fills (objectBoundingBox).
func vectorGradient(g rasterx.Gradient, opacity float64, m rasterx.Matrix2D, box [4]float64) (*VectorGradient, bool) {
	if len(g.Stops) == 0 {
		return nil, true
	}
	exact := g.Spread == rasterx.PadSpread
	stops := append([]rasterx.GradStop(nil), g.Stops...)
	sort.SliceStable(stops, func(i, j int) bool { return stops[i].Offset < stops[j].Offset })
	v := &VectorGradient{Radial: g.IsRadial}
	for _, st := range stops {
		v.Stops = append(v.Stops, VectorStop{Offset: math.Max(0, math.Min(1, st.Offset)), Color: rasterx.ApplyOpacity(st.StopColor, st.Opacity*opacity)})
		if st.Opacity != stops[0].Opacity {
			exact = false // one opacity per shading
		}
	}
	// The gradient's own space to the element's user space: for
	// objectBoundingBox, the box, then gradientTransform within it, as
	// rasterx does (translate(box) scale(box) Matrix scale(1/box)
	// translate(-box)); for userSpaceOnUse, gradientTransform.
	gm := g.Matrix
	if gm == (rasterx.Matrix2D{}) {
		gm = rasterx.Identity
	}
	var toUser rasterx.Matrix2D
	if g.Units == rasterx.ObjectBoundingBox {
		x, y, w, h := box[0], box[1], box[2]-box[0], box[3]-box[1]
		if !(w > 0) || !(h > 0) {
			return v, exact
		}
		toUser = rasterx.Identity.Translate(x, y).Scale(w, h).Mult(gm)
	} else {
		toUser = gm
	}
	full := m.Mult(toUser)
	if g.IsRadial {
		cx, cy, fx, fy, r := g.Points[0], g.Points[1], g.Points[2], g.Points[3], g.Points[4]
		if cx != fx || cy != fy {
			exact = false // a focal point: no form here
		}
		if math.Abs(full.B) > 1e-9 || math.Abs(full.C) > 1e-9 {
			exact = false // rotated or skewed: not an axis-aligned ellipse
		}
		v.CX, v.CY = full.Transform(cx, cy)
		v.RX, v.RY = math.Abs(full.A*r), math.Abs(full.D*r)
		return v, exact
	}
	if !similarity(full) {
		exact = false // a skew makes the iso-lines oblique to the axis
	}
	v.X1, v.Y1 = full.Transform(g.Points[0], g.Points[1])
	v.X2, v.Y2 = full.Transform(g.Points[2], g.Points[3])
	return v, exact
}

// similarity reports whether m keeps angles: a rotation, a uniform scale, a
// reflection and a translation.
func similarity(m rasterx.Matrix2D) bool {
	const eps = 1e-9
	return (math.Abs(m.A-m.D) < eps && math.Abs(m.B+m.C) < eps) || (math.Abs(m.A+m.D) < eps && math.Abs(m.B-m.C) < eps)
}

// pathBounds is the bounding box of the control points of a path given in
// viewBox units, mapped back to the user space of the element by m's
// inverse: [minX, minY, maxX, maxY].
func pathBounds(ops []PathOp, m rasterx.Matrix2D) [4]float64 {
	inv := m.Invert()
	b := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	n := map[byte]int{'M': 1, 'L': 1, 'Q': 2, 'C': 3}
	for _, op := range ops {
		for i := 0; i < n[op.Op]; i++ {
			x, y := inv.Transform(op.Pts[i][0], op.Pts[i][1])
			b[0], b[1] = math.Min(b[0], x), math.Min(b[1], y)
			b[2], b[3] = math.Max(b[2], x), math.Max(b[3], y)
		}
	}
	return b
}

// pathRecorder is a rasterx.Adder that keeps the path it is given, in
// float64, under a transform.
type pathRecorder struct {
	m   rasterx.Matrix2D
	ops []PathOp
}

func (r *pathRecorder) pt(p fixed.Point26_6) [2]float64 {
	x, y := r.m.Transform(float64(p.X)/64, float64(p.Y)/64)
	return [2]float64{x, y}
}

func (r *pathRecorder) Start(a fixed.Point26_6) {
	r.ops = append(r.ops, PathOp{Op: 'M', Pts: [3][2]float64{r.pt(a)}})
}

func (r *pathRecorder) Line(b fixed.Point26_6) {
	r.ops = append(r.ops, PathOp{Op: 'L', Pts: [3][2]float64{r.pt(b)}})
}

func (r *pathRecorder) QuadBezier(b, c fixed.Point26_6) {
	r.ops = append(r.ops, PathOp{Op: 'Q', Pts: [3][2]float64{r.pt(b), r.pt(c)}})
}

func (r *pathRecorder) CubeBezier(b, c, d fixed.Point26_6) {
	r.ops = append(r.ops, PathOp{Op: 'C', Pts: [3][2]float64{r.pt(b), r.pt(c), r.pt(d)}})
}

func (r *pathRecorder) Stop(closeLoop bool) {
	if closeLoop {
		r.ops = append(r.ops, PathOp{Op: 'Z'})
	}
}

// transformPath is a path in viewBox units: p under m.
func transformPath(p rasterx.Path, m rasterx.Matrix2D) []PathOp {
	r := &pathRecorder{m: m}
	p.AddTo(r)
	return r.ops
}

// outlineScanner is a rasterx.Scanner that keeps the polygons it is given
// (a stroke's outline, as the stroker emits it) instead of filling them.
type outlineScanner struct {
	inv  float64 // back to viewBox units
	ops  []PathOp
	open bool
}

func (s *outlineScanner) pt(p fixed.Point26_6) [2]float64 {
	return [2]float64{float64(p.X) / 64 * s.inv, float64(p.Y) / 64 * s.inv}
}

func (s *outlineScanner) Start(a fixed.Point26_6) {
	if s.open {
		s.ops = append(s.ops, PathOp{Op: 'Z'})
	}
	s.ops = append(s.ops, PathOp{Op: 'M', Pts: [3][2]float64{s.pt(a)}})
	s.open = true
}

func (s *outlineScanner) Line(b fixed.Point26_6) {
	s.ops = append(s.ops, PathOp{Op: 'L', Pts: [3][2]float64{s.pt(b)}})
}

func (s *outlineScanner) Draw() {
	if s.open {
		s.ops = append(s.ops, PathOp{Op: 'Z'})
		s.open = false
	}
}

func (s *outlineScanner) GetPathExtent() fixed.Rectangle26_6 { return fixed.Rectangle26_6{} }
func (s *outlineScanner) SetBounds(int, int)                 {}
func (s *outlineScanner) SetColor(interface{})               {}
func (s *outlineScanner) SetWinding(bool)                    {}
func (s *outlineScanner) Clear()                             {}
func (s *outlineScanner) SetClip(image.Rectangle)            {}

// strokeOutline is the outline of the path's stroke in viewBox units, made
// by rasterx's stroker as Draw makes it, at k fixed-point units per viewBox
// unit.
func (p *SvgPath) strokeOutline(k float64) []PathOp {
	sc := &outlineScanner{inv: 1 / k}
	d := rasterx.NewDasher(1, 1, sc)
	lineGap, lineCap := p.LineGap, p.LineCap
	if lineGap == nil {
		lineGap = DefaultStyle.LineGap
	}
	if lineCap == nil {
		lineCap = DefaultStyle.LineCap
	}
	leadLineCap := lineCap
	if p.LeadLineCap != nil {
		leadLineCap = p.LeadLineCap
	}
	// The stroke is drawn in the scaled space: width, dashes and offset are
	// in the element's user units, scaled with the element's transform by
	// the stroker itself (it strokes the transformed path), so the
	// transform here is the element's, then k.
	scale := math.Sqrt(math.Abs(p.mAdder.M.A*p.mAdder.M.D - p.mAdder.M.B*p.mAdder.M.C))
	dash := make([]float64, len(p.Dash))
	for i, v := range p.Dash {
		dash[i] = v * scale * k
	}
	d.SetStroke(fixed.Int26_6(p.LineWidth*scale*k*64), fixed.Int26_6(p.MiterLimit*64),
		leadLineCap, lineCap, lineGap, p.LineJoin, dash, p.DashOffset*scale*k)
	ma := rasterx.MatrixAdder{Adder: d, M: rasterx.Identity.Scale(k, k).Mult(p.mAdder.M)}
	p.Path.AddTo(&ma)
	d.Draw()
	return sc.ops
}

// vectorClips resolves the clip-path properties in force on the path to clip
// paths in viewBox units. ok is false when one has no exact form; gone when
// one keeps nothing. A clip that cannot be applied (it names no clipPath, or
// is circular) is skipped, as Draw skips it.
func (p *SvgPath) vectorClips() (clips []ClipPath, ok, gone bool) {
	ok = true
	for _, cu := range p.clips {
		cs, cok, cgone := cu.vector(cu.m, cu.box, map[*clipDef]bool{})
		if !cok {
			ok = false
		}
		if cgone {
			return nil, ok, true
		}
		clips = append(clips, cs...)
	}
	return clips, ok, false
}

// vector is the clip as clip paths in viewBox units: the union of its
// silhouettes, then its own clip-path's.
func (cu *clipUse) vector(d rasterx.Matrix2D, box bounds, visiting map[*clipDef]bool) (clips []ClipPath, ok, gone bool) {
	ok = true
	if cu.icon == nil {
		return nil, true, false
	}
	def := cu.icon.clips[cu.id]
	if def == nil || visiting[def] || len(visiting) >= maxClipDepth {
		return nil, true, false
	}
	visiting[def] = true
	defer delete(visiting, def)
	content := d.Mult(def.transform)
	if def.bboxUnits {
		w, h := box.maxX-box.minX, box.maxY-box.minY
		if !box.ok || !(w > 0) || !(h > 0) {
			return nil, true, true
		}
		content = content.Mult(rasterx.Matrix2D{A: w, D: h, E: box.minX, F: box.minY})
	}
	var union ClipPath
	for i := range def.shapes {
		sh := &def.shapes[i]
		if len(sh.clips) > 0 {
			ok = false // a clipped silhouette: not one path
		}
		if i == 0 {
			union.EvenOdd = sh.evenOdd
		} else if sh.evenOdd != union.EvenOdd {
			ok = false // mixed rules: not one path
		}
		union.Path = append(union.Path, transformPath(sh.path, content.Mult(sh.m))...)
	}
	if len(union.Path) == 0 {
		return nil, ok, true
	}
	clips = append(clips, union)
	for _, nested := range def.clips {
		ns, nok, ngone := nested.vector(d, box, visiting)
		if !nok {
			ok = false
		}
		if ngone {
			return nil, ok, true
		}
		clips = append(clips, ns...)
	}
	return clips, ok, false
}
