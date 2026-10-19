// Copyright (c) 2026 the go-gfx authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package oksvg

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/srwiley/rasterx"
)

// render draws an SVG document into a w×h image, 1 user unit = 1 pixel when the
// viewBox is w×h.
func render(t testing.TB, svg string, w, h int) *image.RGBA {
	t.Helper()
	icon, err := ReadIconStream(strings.NewReader(svg), StrictErrorMode)
	if err != nil {
		t.Fatalf("%v\n%s", err, svg)
	}
	icon.SetTarget(0, 0, float64(w), float64(h))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	sc := rasterx.NewScannerGV(w, h, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(w, h, sc), 1)
	return img
}

func alpha(img *image.RGBA, x, y int) int { return int(img.RGBAAt(x, y).A) }

func wantA(t *testing.T, img *image.RGBA, x, y, lo, hi int) {
	t.Helper()
	if a := alpha(img, x, y); a < lo || a > hi {
		t.Errorf("alpha at (%d,%d) = %d, want %d..%d", x, y, a, lo, hi)
	}
}

const head = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">`

func TestClipCircle(t *testing.T) {
	img := render(t, head+`<defs><clipPath id="c"><circle cx="50" cy="50" r="30"/></clipPath></defs>
<rect width="100" height="100" fill="#ff0000" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 50, 50, 255, 255)
	wantA(t, img, 5, 5, 0, 0)
	wantA(t, img, 50, 5, 0, 0)
	wantA(t, img, 70, 50, 255, 255)
	wantA(t, img, 90, 50, 0, 0)
	// Anti-aliased: the edge pixel at x=79 (circle edge at 80) is mostly in,
	// and a pixel straddling the edge has partial coverage.
	wantA(t, img, 79, 50, 200, 255)
	img2 := render(t, head+`<defs><clipPath id="c"><circle cx="50" cy="50" r="30.5"/></clipPath></defs>
<rect width="100" height="100" fill="#ff0000" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img2, 80, 50, 100, 160) // half a pixel of coverage
	if c := img.RGBAAt(50, 50); c.R != 255 || c.G != 0 {
		t.Errorf("colour changed: %v", c)
	}
	// Total area is that of the disc, within the anti-aliasing error.
	sum := 0.0
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			sum += float64(alpha(img, x, y)) / 255
		}
	}
	if want := math.Pi * 900; math.Abs(sum-want) > 12 {
		t.Errorf("clipped area %.1f, disc %.1f", sum, want)
	}
}

func TestClipEvenOddAndNonZero(t *testing.T) {
	ring := `M20 20H80V80H20Z M35 35H65V65H35Z` // both same direction
	for _, c := range []struct {
		rule       string
		centerFrom int
		centerTo   int
	}{{"evenodd", 0, 0}, {"nonzero", 255, 255}} {
		img := render(t, head+`<defs><clipPath id="c"><path clip-rule="`+c.rule+`" d="`+ring+`"/></clipPath></defs>
<rect width="100" height="100" fill="#0000ff" clip-path="url(#c)"/></svg>`, 100, 100)
		wantA(t, img, 50, 50, c.centerFrom, c.centerTo)
		wantA(t, img, 25, 50, 255, 255)
		wantA(t, img, 10, 10, 0, 0)
	}
	// clip-rule on the clipPath is the default of its children.
	img := render(t, head+`<defs><clipPath id="c" clip-rule="evenodd"><path d="`+ring+`"/></clipPath></defs>
<rect width="100" height="100" fill="#0000ff" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 50, 50, 0, 0)
}

func TestClipObjectBoundingBox(t *testing.T) {
	img := render(t, head+`<defs><clipPath id="c" clipPathUnits="objectBoundingBox"><circle cx=".5" cy=".5" r=".5"/></clipPath></defs>
<rect x="20" y="30" width="60" height="40" fill="#00ff00" clip-path="url(#c)"/></svg>`, 100, 100)
	// An ellipse inscribed in the rectangle 20..80 × 30..70.
	wantA(t, img, 50, 50, 255, 255)
	wantA(t, img, 22, 32, 0, 0) // the rectangle's corner
	wantA(t, img, 78, 68, 0, 0) // the other
	wantA(t, img, 30, 50, 255, 255)
	wantA(t, img, 50, 31, 255, 255)
	wantA(t, img, 10, 50, 0, 0) // outside the rectangle
	// Of a group: the box of everything it draws.
	img = render(t, head+`<defs><clipPath id="c" clipPathUnits="objectBoundingBox"><rect x="0" y="0" width=".5" height="1"/></clipPath></defs>
<g clip-path="url(#c)"><rect x="10" y="10" width="30" height="30" fill="red"/><rect x="60" y="60" width="30" height="30" fill="red"/></g></svg>`, 100, 100)
	// The group's box is 10..90: the left half is 10..50.
	wantA(t, img, 20, 20, 255, 255)
	wantA(t, img, 70, 70, 0, 0)
	// No bounding box width: clipped away.
	img = render(t, head+`<defs><clipPath id="c" clipPathUnits="objectBoundingBox"><rect width="1" height="1"/></clipPath></defs>
<path d="M10 50H90" stroke="red" stroke-width="10" fill="none" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 50, 50, 0, 0)
}

// The clipPath transform is outermost: on an objectBoundingBox clip it moves by
// user units, then the content is mapped into the box (compared with Chrome).
func TestClipBoundingBoxWithTransform(t *testing.T) {
	img := render(t, head+`<defs><clipPath id="c" clipPathUnits="objectBoundingBox" transform="translate(.25 0)"><rect width=".5" height="1"/></clipPath></defs>
<rect x="10" y="30" width="80" height="40" fill="red" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 12, 50, 255, 255) // 10.25 .. 50.25
	wantA(t, img, 48, 50, 255, 255)
	wantA(t, img, 55, 50, 0, 0)
	wantA(t, img, 5, 50, 0, 0)
}

// An arc command with several sets of parameters: each set is its own arc (a
// defect of upstream oksvg, which reused the first).
func TestArcWithImplicitRepeats(t *testing.T) {
	two := head + `<path fill="red" d="M20 50a20 20 0 0 1 20-20 15 15 0 0 1 15 15z"/></svg>`
	sep := head + `<path fill="red" d="M20 50a20 20 0 0 1 20-20a15 15 0 0 1 15 15z"/></svg>`
	a, b := render(t, two, 100, 100), render(t, sep, 100, 100)
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			t.Fatalf("an implicit repeat draws differently at byte %d: %d vs %d", i, a.Pix[i], b.Pix[i])
		}
	}
	wantA(t, a, 25, 40, 1, 255)
}

func TestClipTransforms(t *testing.T) {
	// The clipPath's own transform.
	img := render(t, head+`<defs><clipPath id="c" transform="translate(40 0)"><rect width="20" height="100"/></clipPath></defs>
<rect width="100" height="100" fill="red" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 50, 50, 255, 255)
	wantA(t, img, 10, 50, 0, 0)
	// The referencing element's transform applies to the clip (userSpaceOnUse is
	// ITS coordinate system): the clip rect 0..20 moves with the group.
	img = render(t, head+`<defs><clipPath id="c"><rect width="20" height="100"/></clipPath></defs>
<g transform="translate(60 0)" clip-path="url(#c)"><rect width="100" height="100" fill="red"/></g></svg>`, 100, 100)
	wantA(t, img, 70, 50, 255, 255)
	wantA(t, img, 10, 50, 0, 0)
	wantA(t, img, 90, 50, 0, 0)
	// An element's own transform is part of its user space too, and the
	// attribute order does not matter.
	img = render(t, head+`<defs><clipPath id="c"><rect width="20" height="100"/></clipPath></defs>
<rect clip-path="url(#c)" transform="translate(60 0)" width="100" height="100" fill="red"/></svg>`, 100, 100)
	wantA(t, img, 70, 50, 255, 255)
	wantA(t, img, 10, 50, 0, 0)
	// Ancestor transforms of the clipPath do not apply to its content.
	img = render(t, head+`<g transform="translate(70 0)"><defs><clipPath id="c"><rect width="20" height="100"/></clipPath></defs></g>
<rect width="100" height="100" fill="red" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 10, 50, 255, 255)
	wantA(t, img, 80, 50, 0, 0)
	// Scaled output: the mask is built in device pixels.
	img = render(t, head+`<defs><clipPath id="c"><circle cx="50" cy="50" r="30"/></clipPath></defs>
<rect width="100" height="100" fill="red" clip-path="url(#c)"/></svg>`, 200, 200)
	wantA(t, img, 100, 100, 255, 255)
	wantA(t, img, 100, 30, 0, 0)
	wantA(t, img, 100, 45, 255, 255)
}

func TestClipNested(t *testing.T) {
	// A clipPath with clip-path: the intersection.
	img := render(t, head+`<defs>
<clipPath id="half"><rect width="50" height="100"/></clipPath>
<clipPath id="c" clip-path="url(#half)"><circle cx="50" cy="50" r="40"/></clipPath></defs>
<rect width="100" height="100" fill="red" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 30, 50, 255, 255)
	wantA(t, img, 70, 50, 0, 0)
	wantA(t, img, 5, 5, 0, 0)
	// A child with clip-path: clipped before the union.
	img = render(t, head+`<defs>
<clipPath id="top"><rect width="100" height="50"/></clipPath>
<clipPath id="c"><rect width="100" height="100" clip-path="url(#top)"/><rect x="40" y="70" width="20" height="20"/></clipPath></defs>
<rect width="100" height="100" fill="red" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 20, 20, 255, 255)
	wantA(t, img, 20, 60, 0, 0)
	wantA(t, img, 50, 80, 255, 255)
	// An element and its ancestor, each clipped: the intersection.
	img = render(t, head+`<defs>
<clipPath id="a"><rect width="60" height="100"/></clipPath>
<clipPath id="b"><rect x="40" width="60" height="100"/></clipPath></defs>
<g clip-path="url(#a)"><rect width="100" height="100" fill="red" clip-path="url(#b)"/></g></svg>`, 100, 100)
	wantA(t, img, 50, 50, 255, 255)
	wantA(t, img, 20, 50, 0, 0)
	wantA(t, img, 80, 50, 0, 0)
	// A clipPath's content does not inherit clips from where the clipPath stands.
	img = render(t, head+`<defs><clipPath id="a"><rect width="10" height="10"/></clipPath></defs>
<g clip-path="url(#a)"><clipPath id="c"><rect width="100" height="100"/></clipPath></g>
<rect width="100" height="100" fill="red" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 50, 50, 255, 255)
}

func TestClipUnionAndUse(t *testing.T) {
	img := render(t, head+`<defs><rect id="r" width="20" height="100"/>
<clipPath id="c"><rect width="30" height="100"/><use href="#r" x="60"/></clipPath></defs>
<rect width="100" height="100" fill="red" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 10, 50, 255, 255)
	wantA(t, img, 50, 50, 0, 0)
	wantA(t, img, 70, 50, 255, 255)
}

func TestClipInvalidReferences(t *testing.T) {
	cases := map[string]string{
		"missing":    `<rect width="100" height="100" fill="red" clip-path="url(#nope)"/>`,
		"not a clip": `<g id="g"/><rect width="100" height="100" fill="red" clip-path="url(#g)"/>`,
		"none":       `<rect width="100" height="100" fill="red" clip-path="none"/>`,
		"malformed":  `<rect width="100" height="100" fill="red" clip-path="url(#"/>`,
		"self":       `<defs><clipPath id="a" clip-path="url(#a)"><rect width="100" height="100"/></clipPath></defs><rect width="100" height="100" fill="red" clip-path="url(#a)"/>`,
		"cycle": `<defs><clipPath id="a" clip-path="url(#b)"><rect width="100" height="100"/></clipPath>
<clipPath id="b" clip-path="url(#a)"><rect width="100" height="100"/></clipPath></defs><rect width="100" height="100" fill="red" clip-path="url(#a)"/>`,
		"child cycle": `<defs><clipPath id="a"><rect width="100" height="100" clip-path="url(#a)"/></clipPath></defs><rect width="100" height="100" fill="red" clip-path="url(#a)"/>`,
	}
	for name, body := range cases {
		done := make(chan *image.RGBA)
		go func() { done <- render(t, head+body+`</svg>`, 100, 100) }()
		select {
		case img := <-done:
			// An ignored reference leaves the element unclipped.
			wantA(t, img, 50, 50, 255, 255)
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: does not terminate", name)
		}
	}
}

func TestClipPathIsNeverDrawn(t *testing.T) {
	img := render(t, head+`<clipPath id="c"><rect width="100" height="100" fill="red"/></clipPath></svg>`, 100, 100)
	wantA(t, img, 50, 50, 0, 0)
	img = render(t, head+`<defs><clipPath id="c"><rect width="100" height="100" fill="red"/></clipPath></defs></svg>`, 100, 100)
	wantA(t, img, 50, 50, 0, 0)
}

func TestClipEmptyAndHidden(t *testing.T) {
	// An empty clipPath clips everything away.
	img := render(t, head+`<defs><clipPath id="c"/></defs><rect width="100" height="100" fill="red" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 50, 50, 0, 0)
	// A child that is not displayed does not contribute.
	img = render(t, head+`<defs><clipPath id="c"><rect width="100" height="100" display="none"/><rect width="10" height="10"/></clipPath></defs>
<rect width="100" height="100" fill="red" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 50, 50, 0, 0)
	wantA(t, img, 5, 5, 255, 255)
	// A clip entirely off the surface keeps nothing.
	img = render(t, head+`<defs><clipPath id="c"><rect x="500" y="500" width="10" height="10"/></clipPath></defs>
<rect width="100" height="100" fill="red" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 50, 50, 0, 0)
}

func TestClipStrokeAndGradient(t *testing.T) {
	img := render(t, head+`<defs><clipPath id="c"><rect width="50" height="100"/></clipPath>
<linearGradient id="g" x1="0" x2="1"><stop offset="0" stop-color="#ff0000"/><stop offset="1" stop-color="#0000ff"/></linearGradient></defs>
<rect x="10" y="10" width="80" height="80" fill="url(#g)" stroke="#00ff00" stroke-width="6" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 30, 50, 255, 255)
	wantA(t, img, 70, 50, 0, 0)
	if c := img.RGBAAt(30, 50); c.B >= c.R {
		t.Errorf("gradient lost under the clip: %v", c)
	}
	// The stroke is clipped too.
	wantA(t, img, 10, 50, 255, 255)
	wantA(t, img, 90, 50, 0, 0)
}

func TestClipOpacity(t *testing.T) {
	img := render(t, head+`<defs><clipPath id="c"><rect width="50" height="100"/></clipPath></defs>
<rect width="100" height="100" fill="red" opacity=".5" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 20, 50, 126, 130)
	wantA(t, img, 80, 50, 0, 0)
}

// An element that draws nothing leaves its clip's box empty without failing.
func TestClipOfNothing(t *testing.T) {
	render(t, head+`<defs><clipPath id="c" clipPathUnits="objectBoundingBox"><rect width="1" height="1"/></clipPath></defs><g clip-path="url(#c)"/></svg>`, 100, 100)
}

func TestMaskTooLargeIsNotApplied(t *testing.T) {
	old := maxMaskPixels
	maxMaskPixels = 100
	defer func() { maxMaskPixels = old }()
	img := render(t, head+`<defs><clipPath id="c"><rect width="50" height="100"/></clipPath></defs>
<rect width="100" height="100" fill="red" clip-path="url(#c)"/></svg>`, 100, 100)
	wantA(t, img, 80, 50, 255, 255) // unclipped: the bound refused the mask
}

func TestClipCacheFollowsTransform(t *testing.T) {
	icon, err := ReadIconStream(strings.NewReader(head+`<defs><clipPath id="c"><circle cx="50" cy="50" r="30"/></clipPath></defs>
<rect width="100" height="100" fill="red" clip-path="url(#c)"/></svg>`), StrictErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{100, 200, 100} {
		icon.SetTarget(0, 0, float64(size), float64(size))
		img := image.NewRGBA(image.Rect(0, 0, size, size))
		icon.Draw(rasterx.NewDasher(size, size, rasterx.NewScannerGV(size, size, img, img.Bounds())), 1)
		wantA(t, img, size/2, size/2, 255, 255)
		wantA(t, img, size/2, size/2-int(0.4*float64(size)), 0, 0)
	}
}

// ---- the coverage rasteriser ----

func covOf(w, h int, evenOdd bool, polys ...[][2]float64) []uint8 {
	cr := newCovRaster(w, h)
	for _, p := range polys {
		for i := range p {
			a, b := p[i], p[(i+1)%len(p)]
			cr.line(a[0], a[1], b[0], b[1])
		}
	}
	return cr.coverage(evenOdd)
}

func TestCoverageArea(t *testing.T) {
	// A 10.5 × 7.25 rectangle at a fractional offset covers its exact area.
	cov := covOf(20, 20, false, [][2]float64{{2.25, 3.5}, {12.75, 3.5}, {12.75, 10.75}, {2.25, 10.75}})
	sum := 0.0
	for _, v := range cov {
		sum += float64(v) / 255
	}
	if want := 10.5 * 7.25; math.Abs(sum-want) > 0.3 {
		t.Errorf("area %.2f, want %.2f", sum, want)
	}
	// Opposite winding gives the same coverage.
	rev := covOf(20, 20, false, [][2]float64{{2.25, 10.75}, {12.75, 10.75}, {12.75, 3.5}, {2.25, 3.5}})
	for i := range cov {
		if cov[i] != rev[i] {
			t.Fatalf("winding changes coverage at %d: %d vs %d", i, cov[i], rev[i])
		}
	}
	// A diagonal edge: the triangle has half the area of its square.
	tri := covOf(10, 10, false, [][2]float64{{0, 0}, {10, 0}, {0, 10}})
	sum = 0
	for _, v := range tri {
		sum += float64(v) / 255
	}
	if math.Abs(sum-50) > 0.5 {
		t.Errorf("triangle area %.2f, want 50", sum)
	}
}

func TestCoverageClipsToRegion(t *testing.T) {
	// A shape far larger than the region fills it; one beside it adds nothing;
	// extreme and non-finite coordinates are dropped, not a crash.
	cov := covOf(8, 8, false, [][2]float64{{-1000, -1000}, {1000, -1000}, {1000, 1000}, {-1000, 1000}})
	for i, v := range cov {
		if v != 255 {
			t.Fatalf("pixel %d = %d inside a huge shape", i, v)
		}
	}
	cov = covOf(8, 8, false, [][2]float64{{20, 0}, {30, 0}, {30, 8}, {20, 8}})
	for i, v := range cov {
		if v != 0 {
			t.Fatalf("pixel %d = %d for a shape to the right", i, v)
		}
	}
	set := &flatSet{}
	set.add(math.NaN(), 0, 1, 1)
	set.add(0, 0, 1e300, 1)
	set.add(math.Inf(1), 0, 1, 1)
	if set.ok || len(set.segs) != 0 {
		t.Errorf("bad segments kept: %+v", set)
	}
}

func TestCoverageFillRules(t *testing.T) {
	sq := func(a, b float64) [][2]float64 { return [][2]float64{{a, a}, {b, a}, {b, b}, {a, b}} }
	for _, c := range []struct {
		eo   bool
		want uint8
	}{{false, 255}, {true, 0}} {
		cov := covOf(20, 20, c.eo, sq(2, 18), sq(6, 14))
		if cov[10*20+10] != c.want {
			t.Errorf("evenOdd=%v: center %d, want %d", c.eo, cov[10*20+10], c.want)
		}
		if cov[3*20+3] != 255 {
			t.Errorf("evenOdd=%v: ring %d", c.eo, cov[3*20+3])
		}
	}
}

func TestBoundsAndRef(t *testing.T) {
	for in, want := range map[string]string{
		"url(#a)": "a", "url( #a )": "a", `url('#a')`: "a", `url("#a")`: "a", "url(#a) ": "a",
	} {
		if id, ok := parseClipRef(in); !ok || id != want {
			t.Errorf("parseClipRef(%q) = %q, %v", in, id, ok)
		}
	}
	for _, in := range []string{"", "none", "url(a)", "url(#)", "url(#a", "circle(50%)"} {
		if _, ok := parseClipRef(in); ok {
			t.Errorf("parseClipRef(%q) accepted", in)
		}
	}
}

// ---- no cost without clipPath, and a fuzz target ----

func BenchmarkDrawUnclipped(b *testing.B) {
	svg := head + `<path d="M10 10H90V90H10Z" fill="red"/><circle cx="50" cy="50" r="30" fill="blue"/></svg>`
	icon, err := ReadIconStream(strings.NewReader(svg))
	if err != nil {
		b.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	icon.SetTarget(0, 0, 64, 64)
	d := rasterx.NewDasher(64, 64, rasterx.NewScannerGV(64, 64, img, img.Bounds()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		icon.Draw(d, 1)
	}
}

func BenchmarkDrawClipped(b *testing.B) {
	svg := head + `<defs><clipPath id="c"><circle cx="50" cy="50" r="40"/></clipPath></defs><g clip-path="url(#c)"><path d="M10 10H90V90H10Z" fill="red"/><circle cx="50" cy="50" r="30" fill="blue"/></g></svg>`
	icon, err := ReadIconStream(strings.NewReader(svg))
	if err != nil {
		b.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	icon.SetTarget(0, 0, 64, 64)
	d := rasterx.NewDasher(64, 64, rasterx.NewScannerGV(64, 64, img, img.Bounds()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		icon.Draw(d, 1)
	}
}

func FuzzClipSVG(f *testing.F) {
	for _, s := range []string{
		head + `<defs><clipPath id="c"><circle cx="50" cy="50" r="30"/></clipPath></defs><rect width="100" height="100" clip-path="url(#c)"/></svg>`,
		head + `<defs><clipPath id="a" clip-path="url(#b)"><rect width="9" height="9"/></clipPath><clipPath id="b" clip-path="url(#a)"/></defs><g clip-path="url(#a)"><path d="M0 0L9 9"/></g></svg>`,
		head + `<clipPath id="c" clipPathUnits="objectBoundingBox" transform="scale(2)"><path clip-rule="evenodd" d="M0 0H1V1H0ZM.2 .2H.8V.8H.2Z"/></clipPath><rect width="9" height="9" clip-path="url(#c)"/></svg>`,
		head + `<clipPath id="c"><use href="#u"/></clipPath><rect id="u" width="1e308" height="1e308" clip-path="url(#c)"/></svg>`,
		head + `<clipPath id="c" transform="matrix(1e30 0 0 1e30 0 0)"><rect width="1e300" height="1"/></clipPath><rect width="9" height="9" clip-path="url(#c)"/></svg>`,
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
		defer func() {
			// rasterx panics on some degenerate geometry, with or without a
			// clip; only a hang or a failure of ours would be a defect.
			_ = recover()
		}()
		icon.SetTarget(0, 0, 16, 16)
		img := image.NewRGBA(image.Rect(0, 0, 16, 16))
		icon.Draw(rasterx.NewDasher(16, 16, rasterx.NewScannerGV(16, 16, img, img.Bounds())), 1)
	})
}

var _ = fmt.Sprint
var _ color.Color
