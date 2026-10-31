// Copyright (c) 2026 the go-gfx authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package oksvg

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"

	"github.com/srwiley/rasterx"
	"golang.org/x/image/math/fixed"
)

// pxPerUnit is the scale renderShapes draws at.
var pxPerUnit = 1.0

// rasterPath is a Shape's path as a rasterx path, at pxPerUnit.
func rasterPath(ops []PathOp) rasterx.Path {
	var p rasterx.Path
	f := func(q [2]float64) fixed.Point26_6 {
		return fixed.Point26_6{X: fixed.Int26_6(q[0] * pxPerUnit * 64), Y: fixed.Int26_6(q[1] * pxPerUnit * 64)}
	}
	open := false
	for _, op := range ops {
		switch op.Op {
		case 'M':
			if open {
				p.Stop(false)
			}
			p.Start(f(op.Pts[0]))
			open = true
		case 'L':
			p.Line(f(op.Pts[0]))
		case 'Q':
			p.QuadBezier(f(op.Pts[0]), f(op.Pts[1]))
		case 'C':
			p.CubeBezier(f(op.Pts[0]), f(op.Pts[1]), f(op.Pts[2]))
		case 'Z':
			p.Stop(true)
			open = false
		}
	}
	if open {
		p.Stop(false)
	}
	return p
}

// coverage fills ops into an alpha image.
func coverage(ops []PathOp, evenOdd bool, w, h int) *image.Alpha {
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	sc := rasterx.NewScannerGV(w, h, rgba, rgba.Bounds())
	f := rasterx.NewFiller(w, h, sc)
	f.SetWinding(!evenOdd)
	f.SetColor(color.Black)
	rasterPath(ops).AddTo(f)
	f.Draw()
	a := image.NewAlpha(rgba.Bounds())
	for i := range a.Pix {
		a.Pix[i] = rgba.Pix[4*i+3]
	}
	return a
}

// renderShapes paints Shapes the way a vector writer would: each shape
// filled with its paint, through its clips, over the page.
func renderShapes(t *testing.T, shapes []Shape, w, h int) *image.RGBA {
	t.Helper()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for _, sh := range shapes {
		layer := image.NewRGBA(out.Rect)
		sc := rasterx.NewScannerGV(w, h, layer, layer.Bounds())
		f := rasterx.NewFiller(w, h, sc)
		f.SetWinding(!sh.EvenOdd)
		if g := sh.Gradient; g != nil {
			rg := rasterx.Gradient{IsRadial: g.Radial, Units: rasterx.UserSpaceOnUse, Matrix: rasterx.Identity}
			if g.Radial {
				rg.Points = [5]float64{g.CX, g.CY, g.CX, g.CY, g.RX}
			} else {
				rg.Points = [5]float64{g.X1, g.Y1, g.X2, g.Y2}
			}
			for _, st := range g.Stops {
				c := st.Color
				op := float64(c.A) / 255
				c.A = 255
				rg.Stops = append(rg.Stops, rasterx.GradStop{StopColor: c, Offset: st.Offset, Opacity: op})
			}
			f.SetColor(rg.GetColorFunction(1))
		} else {
			f.SetColor(sh.Color)
		}
		// even-odd through oksvg's own coverage filler, as Draw fills it
		// (rasterx's even-odd rule is not right)
		if !sh.EvenOdd || !fillEvenOdd(f.Scanner, rasterPath(sh.Path), rasterx.Identity) {
			rasterPath(sh.Path).AddTo(f)
			f.Draw()
		}
		mask := image.NewAlpha(out.Rect)
		draw.Draw(mask, mask.Rect, image.Opaque, image.Point{}, draw.Src)
		for _, cl := range sh.Clips {
			cov := coverage(cl.Path, cl.EvenOdd, w, h)
			for i := range mask.Pix {
				mask.Pix[i] = uint8(int(mask.Pix[i]) * int(cov.Pix[i]) / 255)
			}
		}
		draw.DrawMask(out, out.Rect, layer, image.Point{}, mask, image.Point{}, draw.Over)
	}
	return out
}

// diff is the fraction of pixels where a channel differs by more than tol.
func diff(a, b *image.RGBA, tol int) float64 {
	n := 0
	for i := 0; i < len(a.Pix); i += 4 {
		for k := 0; k < 4; k++ {
			d := int(a.Pix[i+k]) - int(b.Pix[i+k])
			if d > tol || -d > tol {
				n++
				break
			}
		}
	}
	return float64(n) / float64(len(a.Pix)/4)
}

func shapesOf(t *testing.T, svg string) ([]Shape, bool) {
	t.Helper()
	icon, err := ReadIconStream(strings.NewReader(svg), StrictErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	return icon.Shapes()
}

// Shapes, painted as a vector writer paints them, are what Draw paints:
// fills (curves, transforms, fill rules), strokes (width, caps, joins,
// dashes, under a scale), gradients (bounding box and user space, a
// gradientTransform), clips (user space, bounding box, a union of
// silhouettes, a clipPath with its own clip-path), opacities.
func TestShapesPaintWhatDrawPaints(t *testing.T) {
	for name, body := range map[string]string{
		"fills": `<g transform="translate(5 5) rotate(10 50 50)"><path d="M10 10 C 40 0 60 40 90 10 L 80 90 Q 50 60 20 90 Z" fill="#c33"/>` +
			`<path fill-rule="evenodd" d="M30 30 h40 v40 h-40 Z M40 40 h20 v20 h-20 Z" fill="#36c" opacity=".7"/></g>`,
		"strokes": `<path d="M10 20 L50 60 L90 20" fill="none" stroke="#080" stroke-width="8" stroke-linejoin="round" stroke-linecap="round"/>` +
			`<g transform="scale(0.5)"><path d="M20 160 H180" stroke="#00f" stroke-width="10" stroke-dasharray="20 10" stroke-linecap="square"/></g>` +
			`<circle cx="70" cy="70" r="15" fill="none" stroke="#f80" stroke-width="3"/>`,
		"gradients": `<defs><linearGradient id="a" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></linearGradient>` +
			`<linearGradient id="b" gradientUnits="userSpaceOnUse" x1="0" y1="50" x2="0" y2="100" gradientTransform="translate(0 -5)"><stop offset=".2" stop-color="#0f0"/><stop offset=".8" stop-color="#000"/></linearGradient>` +
			`<radialGradient id="c" cx=".5" cy=".5" r=".5"><stop offset="0" stop-color="#ff0"/><stop offset="1" stop-color="#f0f"/></radialGradient></defs>` +
			`<rect x="5" y="5" width="60" height="30" fill="url(#a)"/><rect x="5" y="50" width="40" height="45" fill="url(#b)"/><rect x="55" y="50" width="40" height="40" fill="url(#c)"/>`,
		"clips": `<defs><clipPath id="u"><circle cx="30" cy="30" r="20"/><rect x="40" y="40" width="20" height="20"/></clipPath>` +
			`<clipPath id="b" clipPathUnits="objectBoundingBox"><rect x=".25" y="0" width=".5" height="1"/></clipPath>` +
			`<clipPath id="n" clip-path="url(#u)"><rect x="0" y="20" width="100" height="20"/></clipPath></defs>` +
			`<rect width="100" height="100" fill="#888" clip-path="url(#u)"/><rect x="60" y="60" width="40" height="40" fill="#c00" clip-path="url(#b)"/>` +
			`<g clip-path="url(#n)"><rect width="100" height="100" fill="#06c" opacity=".5"/></g>`,
	} {
		t.Run(name, func(t *testing.T) {
			svg := head + body + `</svg>`
			shapes, exact := shapesOf(t, svg)
			if !exact {
				t.Fatal("not exact")
			}
			want := render(t, svg, 100, 100)
			got := renderShapes(t, shapes, 100, 100)
			if d := diff(want, got, 24); d > 0.01 {
				t.Errorf("%.2f%% of pixels differ", 100*d)
			}
		})
	}
}

// At a scale (a 36-unit viewBox drawn at 72 px, as an emoji is), the shapes
// still paint what Draw paints, strokes included: Draw now scales a stroke
// with the transform, as Shapes does.
func TestShapesAtAScale(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 36 36"><circle cx="18" cy="18" r="16" fill="#fc3"/>` +
		`<path d="M10 22 Q18 30 26 22" fill="none" stroke="#630" stroke-width="2" stroke-linecap="round"/>` +
		`<g transform="translate(18 12) rotate(30)"><rect x="-3" y="-2" width="6" height="4" fill="#630"/></g></svg>`
	shapes, exact := shapesOf(t, svg)
	if !exact {
		t.Fatal("not exact")
	}
	pxPerUnit = 2
	defer func() { pxPerUnit = 1 }()
	want := render(t, svg, 72, 72)
	got := renderShapes(t, shapes, 72, 72)
	if d := diff(want, got, 24); d > 0.01 {
		t.Errorf("%.2f%% of pixels differ", 100*d)
	}
}

// A stroke is as wide as its width in user units under the transform: under
// scale(2), a 4-unit line covers 8 pixels. (scale(2) with one number is a
// uniform scale; it used to flatten y to 0 and draw nothing.)
func TestDrawScalesStrokes(t *testing.T) {
	img := render(t, head+`<g transform="scale(2)"><path d="M0 25 H50" stroke="#000" stroke-width="4"/></g></svg>`, 100, 100)
	for y, want := range map[int]bool{45: false, 47: true, 52: true, 54: false} {
		if got := alpha(img, 50, y) > 200; got != want {
			var col []int
			for yy := 40; yy < 60; yy++ {
				col = append(col, alpha(img, 50, yy))
			}
			t.Errorf("row %d covered %v, want %v (rows 40..59: %v)", y, got, want, col)
		}
	}
}

// A stroke's outline is closed loops: a writer that closes every subpath
// fills it as the scanner does.
func TestStrokeOutlineIsClosedLoops(t *testing.T) {
	shapes, _ := shapesOf(t, head+`<path d="M10 20 L50 60 L90 20" fill="none" stroke="#000" stroke-width="8" stroke-dasharray="5 3"/></svg>`)
	if len(shapes) != 1 {
		t.Fatalf("%d shapes", len(shapes))
	}
	var start [2]float64
	var last [2]float64
	for _, op := range shapes[0].Path {
		switch op.Op {
		case 'M':
			start, last = op.Pts[0], op.Pts[0]
		case 'L':
			last = op.Pts[0]
		case 'Z':
			if last != start {
				t.Fatalf("a loop from %v ends at %v", start, last)
			}
		}
	}
	// the scanner's own entry points draw nothing more
	var sc outlineScanner
	sc.Line(fixed.Point26_6{X: 64})
	sc.SetBounds(1, 1)
	sc.SetColor(nil)
	sc.SetWinding(true)
	sc.Clear()
	sc.SetClip(image.Rectangle{})
	if sc.GetPathExtent() != (fixed.Rectangle26_6{}) {
		t.Error("extent")
	}
	sc.Draw()
	if len(sc.ops) != 0 {
		t.Errorf("a lone point drew %v", sc.ops)
	}
}

// A stroke of more dashes than maxDashes (a hostile dash array: a
// thousandth of a unit along ten thousand units is ten million dashes) has
// no outline, and Draw strokes it solid rather than step through each dash;
// both return at once. So does an outline past maxOutlinePoints; and a dash
// array summing to zero is solid (SVG 1.1 §11.4).
func TestStrokeBounds(t *testing.T) {
	hostile := head + `<path d="M0 50 H10000" fill="none" stroke="#000" stroke-width="4" stroke-dasharray="0.001"/><rect width="10" height="10" fill="#f00"/></svg>`
	shapes, exact := shapesOf(t, hostile)
	if exact || len(shapes) != 1 || shapes[0].Color != (color.NRGBA{R: 255, A: 255}) {
		t.Errorf("exact %v, shapes %v: want inexact and only the square", exact, shapes)
	}
	if img := render(t, hostile, 100, 100); alpha(img, 50, 50) < 200 {
		t.Error("the hostile stroke is not drawn solid")
	}
	zero := head + `<path d="M0 50 H100" stroke="#000" stroke-width="4" stroke-dasharray="0 0"/></svg>`
	if img := render(t, zero, 100, 100); alpha(img, 50, 50) < 200 || alpha(img, 20, 50) < 200 {
		t.Error("a zero dash array is not solid")
	}
	if _, exact := shapesOf(t, zero); exact {
		t.Error("a zero dash array has an exact outline")
	}
	// a short dashed line is within the bounds; a point-length path too
	if _, exact := shapesOf(t, head+`<path d="M0 50 H100" stroke="#000" stroke-dasharray="1"/><path d="M5 5 L5 5" stroke="#000" stroke-dasharray="1"/></svg>`); !exact {
		t.Error("a short dashed line is not exact")
	}
	old := maxOutlinePoints
	maxOutlinePoints = 8
	defer func() { maxOutlinePoints = old }()
	if _, exact := shapesOf(t, head+`<circle cx="50" cy="50" r="40" fill="none" stroke="#000" stroke-width="3"/></svg>`); exact {
		t.Error("an outline past maxOutlinePoints is exact")
	}
}

// FuzzShapes: whatever the document, Shapes returns (a rasterx panic on
// degenerate geometry aside, which the engine recovers from), within the
// bounds: no hang on dashes, finite coordinates only where the input's are.
func FuzzShapes(f *testing.F) {
	for _, s := range []string{
		head + `<path d="M0 50 H10000" fill="none" stroke="#000" stroke-dasharray="0.001"/></svg>`,
		head + `<g transform="scale(1e-9)"><circle cx="5" cy="5" r="4" stroke="#000" stroke-dasharray="1e-12 1e-12"/></g></svg>`,
		head + `<defs><linearGradient id="g" gradientTransform="rotate(30)"><stop offset="0"/><stop offset="2" stop-color="#fff"/></linearGradient></defs><rect width="9" height="9" fill="url(#g)" stroke="url(#g)"/></svg>`,
		head + `<path d="M0 0 Q 1e300 1e300 5 5 C 1 2 3 4 5 6 Z" stroke="#000" stroke-width="1e30"/></svg>`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 1<<14 {
			return
		}
		icon, err := ReadIconStream(strings.NewReader(s))
		if err != nil || icon == nil || icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
			return
		}
		defer func() { _ = recover() }()
		icon.Shapes()
	})
}

// What has no exact form says so; the rest of the drawing is still there.
func TestShapesNotExact(t *testing.T) {
	for name, body := range map[string]string{
		"focal":   `<defs><radialGradient id="g" fx=".2"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></radialGradient></defs><rect width="50" height="50" fill="url(#g)"/>`,
		"reflect": `<defs><linearGradient id="g" spreadMethod="reflect" x2=".3"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></linearGradient></defs><rect width="50" height="50" fill="url(#g)"/>`,
		"alphas":  `<defs><linearGradient id="g"><stop offset="0" stop-color="#f00" stop-opacity=".2"/><stop offset="1" stop-color="#00f"/></linearGradient></defs><rect width="50" height="50" fill="url(#g)"/>`,
		"skew":    `<defs><linearGradient id="g"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></linearGradient></defs><rect width="50" height="50" fill="url(#g)" transform="skewX(20)"/>`,
		"rules":   `<defs><clipPath id="c"><rect width="10" height="10" clip-rule="evenodd"/><rect x="20" width="10" height="10"/></clipPath></defs><rect width="50" height="50" clip-path="url(#c)"/>`,
		"nested":  `<defs><clipPath id="a"><rect width="5" height="5"/></clipPath><clipPath id="c"><rect width="10" height="10" clip-path="url(#a)"/></clipPath></defs><rect width="50" height="50" clip-path="url(#c)"/>`,
	} {
		t.Run(name, func(t *testing.T) {
			shapes, exact := shapesOf(t, head+body+`</svg>`)
			if exact {
				t.Error("exact")
			}
			if len(shapes) != 1 {
				t.Errorf("%d shapes, want the one drawn", len(shapes))
			}
		})
	}
}

// Edge cases: a gradient with no stop paints black, with one its colour; a
// gradient on an empty box and a clip that keeps nothing; a clip naming
// nothing is ignored; a stroke of width 0 is not drawn; Stop without close
// and a quadratic path are kept.
func TestShapesEdges(t *testing.T) {
	shapes, exact := shapesOf(t, head+`<defs><linearGradient id="none"/><linearGradient id="one"><stop offset="0" stop-color="#0f0"/></linearGradient>`+
		`<linearGradient id="g"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></linearGradient>`+
		`<clipPath id="empty"></clipPath><clipPath id="bb" clipPathUnits="objectBoundingBox"><rect width="1" height="1"/></clipPath></defs>`+
		`<rect width="10" height="10" fill="url(#none)"/><rect x="20" width="10" height="10" fill="url(#one)"/>`+
		`<path d="M0 50 H 50" stroke="url(#g)" fill="url(#g)" stroke-width="2"/>`+
		`<rect width="10" height="10" clip-path="url(#empty)"/><path d="M0 0 H10" stroke="#000" clip-path="url(#bb)"/>`+
		`<rect width="10" height="10" clip-path="url(#nothing)"/><path d="M0 0 L10 10" stroke="#000" stroke-width="0" fill="none"/>`+
		`<path d="M0 0 Q 10 10 20 0" fill="#000"/></svg>`)
	if !exact {
		t.Error("exact")
	}
	var kinds []string
	for _, s := range shapes {
		switch {
		case s.Gradient != nil:
			kinds = append(kinds, "grad")
		default:
			kinds = append(kinds, fmt.Sprint(s.Color))
		}
	}
	got := strings.Join(kinds, " ")
	// the fill of the horizontal line has a box of no area: not painted; the
	// clip keeping nothing and the bounding-box clip of that line remove
	// their shapes
	want := "{0 0 0 255} {0 255 0 255} grad {0 0 0 255} {0 0 0 255}"
	if got != want {
		t.Errorf("shapes %s, want %s", got, want)
	}
}
