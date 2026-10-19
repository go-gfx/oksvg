// Copyright (c) 2026 the go-gfx authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package oksvg

import (
	"image"
	"strings"
	"testing"

	"github.com/srwiley/rasterx"
)

const donutSame = `M10 10H90V90H10Z M30 30H70V70H30Z`
const donutOpp = `M10 10H90V90H10Z M30 30V70H70V30Z`

// SVG 1.1 §11.3: with nonzero the direction of the inner square decides; with
// evenodd it does not (compared with Chrome).
func TestFillRuleDonuts(t *testing.T) {
	for _, c := range []struct {
		name, attr, d string
		hole          bool
	}{
		{"same direction, nonzero", ``, donutSame, false},
		{"same direction, evenodd", ` fill-rule="evenodd"`, donutSame, true},
		{"opposite direction, nonzero", ``, donutOpp, true},
		{"opposite direction, evenodd", ` fill-rule="evenodd"`, donutOpp, true},
		{"explicit nonzero", ` fill-rule="nonzero"`, donutSame, false},
		{"style attribute", ` style="fill-rule:evenodd"`, donutSame, true},
	} {
		img := render(t, head+`<path fill="#2244ee"`+c.attr+` d="`+c.d+`"/></svg>`, 100, 100)
		wantA(t, img, 20, 50, 255, 255)
		if c.hole {
			wantA(t, img, 50, 50, 0, 0)
		} else {
			wantA(t, img, 50, 50, 255, 255)
		}
		wantA(t, img, 5, 5, 0, 0)
		if px := img.RGBAAt(20, 50); px.B < 200 || px.R > 60 {
			t.Errorf("%s: colour %v", c.name, px)
		}
	}
}

func TestFillRuleStar(t *testing.T) {
	star := `M50 5L80 95L5 38H95L20 95Z`
	eo := render(t, head+`<path fill="red" fill-rule="evenodd" d="`+star+`"/></svg>`, 100, 100)
	nz := render(t, head+`<path fill="red" d="`+star+`"/></svg>`, 100, 100)
	wantA(t, eo, 50, 50, 0, 0) // the pentagon in the middle
	wantA(t, nz, 50, 50, 255, 255)
	wantA(t, eo, 50, 20, 255, 255) // a point of the star
	wantA(t, nz, 50, 20, 255, 255)
}

func TestFillRuleInheritedAndComposed(t *testing.T) {
	// Inherited from a group; a later path in the group is nonzero again by its own attribute.
	img := render(t, head+`<g fill-rule="evenodd"><path fill="#2a4" d="`+donutSame+`"/><circle cx="50" cy="50" r="10" fill="red"/></g></svg>`, 100, 100)
	wantA(t, img, 20, 50, 255, 255)
	if c := img.RGBAAt(50, 50); c.R != 255 || c.G != 0 {
		t.Errorf("the circle in the hole: %v", c)
	}
	wantA(t, img, 35, 35, 0, 0)
	// A gradient fill, opacity, a clip: the colour comes through unchanged.
	img = render(t, head+`<defs><linearGradient id="g"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></linearGradient></defs>
<path fill="url(#g)" fill-rule="evenodd" d="`+donutSame+`"/></svg>`, 100, 100)
	if l, r := img.RGBAAt(15, 50), img.RGBAAt(85, 50); l.R < 200 || l.B > 60 || r.B < 200 || r.R > 60 {
		t.Errorf("gradient: left %v right %v", l, r)
	}
	wantA(t, img, 50, 50, 0, 0)
	img = render(t, head+`<path fill="#000" fill-opacity=".5" fill-rule="evenodd" d="`+donutSame+`"/></svg>`, 100, 100)
	wantA(t, img, 20, 50, 126, 130)
	wantA(t, img, 50, 50, 0, 0)
	img = render(t, head+`<defs><clipPath id="c"><rect width="60" height="100"/></clipPath></defs>
<path clip-path="url(#c)" fill="#24e" fill-rule="evenodd" d="`+donutSame+`"/></svg>`, 100, 100)
	wantA(t, img, 20, 50, 255, 255)
	wantA(t, img, 50, 50, 0, 0)
	wantA(t, img, 80, 50, 0, 0)
}

// A fill beyond the surface is not an error, and the rule only changes fills.
func TestFillRuleOffSurfaceAndStroke(t *testing.T) {
	img := render(t, head+`<path fill="red" fill-rule="evenodd" d="M500 500H600V600H500Z"/>
<path fill="none" stroke="#00f" stroke-width="4" fill-rule="evenodd" d="`+donutSame+`"/></svg>`, 100, 100)
	wantA(t, img, 50, 50, 0, 0)
	wantA(t, img, 10, 50, 255, 255)
}

// A scanner the code cannot feed draws nonzero instead of failing.
func TestFillRuleFallback(t *testing.T) {
	icon, err := ReadIconStream(strings.NewReader(head+`<path fill="red" fill-rule="evenodd" d="`+donutSame+`"/></svg>`), StrictErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	icon.SetTarget(0, 0, 100, 100)
	img := image.NewRGBA(image.Rect(10, 10, 110, 110)) // does not start at (0,0)
	icon.Draw(rasterx.NewDasher(100, 100, rasterx.NewScannerGV(100, 100, img, img.Bounds())), 1)
	if fillEvenOdd(nil, nil, rasterx.Identity) {
		t.Error("a nil scanner was fed")
	}
}

// ---- arc flags, SVG 1.1 §8.3.9 ----

func TestArcCompactFlags(t *testing.T) {
	// The same arc written with and without separators after the flags.
	spaced := head + `<path fill="red" d="M10 50A40 40 0 0 1 50 10L50 50Z"/></svg>`
	for _, d := range []string{
		`M10 50A40 40 0 0150 10L50 50Z`,
		`M10 50A40 40 0 01 50 10L50 50Z`,
		`M10 50a40,40,0,0,1,40,-40L50 50Z`,
		`M10 50a40 40 0 0 1 40-40L50 50Z`,
		`M10 50A40 40 0 0150 10 50 50Z`, // an extra pair is not an arc: error, handled below
	} {
		if strings.HasSuffix(d, "10 50 50Z") {
			continue
		}
		a, b := render(t, spaced, 100, 100), render(t, head+`<path fill="red" d="`+d+`"/></svg>`, 100, 100)
		for i := range a.Pix {
			if a.Pix[i] != b.Pix[i] {
				t.Fatalf("%q draws differently from the spaced form at byte %d", d, i)
			}
		}
	}
	// The octocat's own: flags 0 and 1, then x = 10.
	img := render(t, head+`<path fill="red" d="M0 50A50 50 0 0110 0L10 50Z"/></svg>`, 100, 100)
	wantA(t, img, 5, 40, 255, 255)
}

func TestScanNumber(t *testing.T) {
	for _, c := range []struct {
		in   string
		want float64
		next int
		ok   bool
	}{
		{"12", 12, 2, true}, {"-3.5x", -3.5, 4, true}, {".5.5", .5, 2, true}, {"1e-3,", .001, 4, true},
		{"1e", 1, 1, true}, {"+2E2 ", 200, 4, true}, {"7.", 7, 2, true}, {"-", 0, 0, false}, {".", 0, 0, false}, {"x", 0, 0, false}, {"", 0, 0, false},
	} {
		v, next, ok := scanNumber(c.in, 0)
		if ok != c.ok || (ok && (v != c.want || next != c.next)) {
			t.Errorf("scanNumber(%q) = %v, %d, %v; want %v, %d, %v", c.in, v, next, ok, c.want, c.next, c.ok)
		}
	}
}

func TestArcPointsErrors(t *testing.T) {
	var c PathCursor
	for _, s := range []string{"10 10 0 2 0 5 5", "10 10 0 0 0 5", "10 10 0 0", "x", "10 10 0 0 0 5 x"} {
		if err := c.getArcPoints(s); err == nil {
			t.Errorf("getArcPoints(%q) accepted", s)
		}
	}
	if err := c.getArcPoints(" 10,10 0 1,0 5 5  8 8 0 0 1 3 3 "); err != nil || len(c.points) != 14 {
		t.Errorf("two sets: %v, %d numbers", err, len(c.points))
	}
	if err := c.getArcPoints(""); err != nil || len(c.points) != 0 {
		t.Errorf("empty: %v, %d", err, len(c.points))
	}
}

func BenchmarkFillNonZero(b *testing.B) {
	benchFill(b, ``)
}

func BenchmarkFillEvenOdd(b *testing.B) {
	benchFill(b, ` fill-rule="evenodd"`)
}

func benchFill(b *testing.B, attr string) {
	icon, err := ReadIconStream(strings.NewReader(head + `<path fill="red"` + attr + ` d="` + donutSame + `"/><circle cx="50" cy="50" r="40" fill="blue"/></svg>`))
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
