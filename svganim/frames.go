package svganim

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/go-gfx/oksvg"
	"github.com/srwiley/rasterx"
)

// Rasterize draws a document as an image of w by h pixels, the whole viewBox
// stretched over it. A root width and height, which oksvg does not read in
// units, are taken off; a viewBox is made from them when there is none.
func Rasterize(svg []byte, w, h int) (img *image.RGBA, err error) {
	if w < 1 || h < 1 || w*h > 1<<26 {
		return nil, errors.New("svganim: the image size is out of bounds")
	}
	defer func() {
		if r := recover(); r != nil { // rasterx panics on some geometry
			img, err = nil, errors.New("svganim: the drawing cannot be rasterized")
		}
	}()
	svg = fixRoot(svg)
	icon, err := oksvg.ReadIconStream(bytes.NewReader(svg), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil, err
	}
	if icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		return nil, errors.New("svganim: the drawing has no viewBox")
	}
	icon.SetTarget(0, 0, float64(w), float64(h))
	img = image.NewRGBA(image.Rect(0, 0, w, h))
	sc := rasterx.NewScannerGV(w, h, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(w, h, sc), 1)
	return img, nil
}

// fixRoot takes width and height off the root element, after making a viewBox
// of them when it has none.
func fixRoot(svg []byte) []byte {
	i := bytes.Index(svg, []byte("<svg"))
	if i < 0 {
		return svg
	}
	j := bytes.IndexByte(svg[i:], '>')
	if j < 0 {
		return svg
	}
	tag := string(svg[i : i+j])
	attr := func(name string) (string, bool) {
		for _, q := range []string{`"`, `'`} {
			if k := strings.Index(tag, " "+name+"="+q); k >= 0 {
				v := tag[k+len(name)+3:]
				if e := strings.Index(v, q); e >= 0 {
					return v[:e], true
				}
			}
		}
		return "", false
	}
	_, hasVB := attr("viewBox")
	w, okW := attr("width")
	h, okH := attr("height")
	out := tag
	if !hasVB && okW && okH {
		wv, hv := ParseValue(w), ParseValue(h)
		if len(wv.Nums) > 0 && len(hv.Nums) > 0 {
			out += ` viewBox="0 0 ` + strconv.FormatFloat(wv.Nums[0], 'g', -1, 64) + ` ` + strconv.FormatFloat(hv.Nums[0], 'g', -1, 64) + `"`
		}
	}
	for _, n := range []string{"width", "height"} {
		for _, q := range []string{`"`, `'`} {
			if k := strings.Index(out, " "+n+"="+q); k >= 0 {
				if e := strings.Index(out[k+len(n)+3:], q); e >= 0 {
					out = out[:k] + out[k+len(n)+3+e+1:]
				}
			}
		}
	}
	return append(append(append([]byte{}, svg[:i]...), out...), svg[i+j:]...)
}

// GIF encodes the frames as an animated GIF: one palette for all of them (the
// median cut of their colours, at most 255 and a transparent one), the frames
// dithered to it, each shown for delayMS milliseconds (rounded to the GIF's
// hundredths of a second, at least one). loop is whether it repeats forever;
// otherwise it plays once.
func GIF(frames []*image.RGBA, delayMS int, loop bool) ([]byte, error) {
	if len(frames) == 0 {
		return nil, errors.New("svganim: no frames")
	}
	b := frames[0].Bounds()
	for _, f := range frames {
		if f.Bounds() != b {
			return nil, errors.New("svganim: the frames differ in size")
		}
	}
	pal := palette(frames)
	delay := int(math.Round(float64(delayMS) / 10))
	if delay < 1 {
		delay = 1
	}
	g := &gif.GIF{LoopCount: 0}
	if !loop {
		g.LoopCount = -1
	}
	for _, f := range frames {
		p := image.NewPaletted(b, pal)
		draw.FloydSteinberg.Draw(p, b, f, b.Min)
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, delay)
		g.Disposal = append(g.Disposal, gif.DisposalBackground)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// palette is a median-cut palette of the frames' colours: entry 0 is
// transparent, the others the average of the boxes the colours were cut into.
func palette(frames []*image.RGBA) color.Palette {
	type bin struct {
		r, g, b uint8
		n       int
	}
	hist := map[uint16]*bin{}
	step := 1
	if px := frames[0].Bounds().Dx() * frames[0].Bounds().Dy() * len(frames); px > 1<<20 {
		step = px >> 20
	}
	k := 0
	for _, f := range frames {
		for i := 0; i+3 < len(f.Pix); i += 4 {
			k++
			if k%step != 0 || f.Pix[i+3] < 128 {
				continue
			}
			a := float64(f.Pix[i+3]) / 255
			r, g, b := uint8(math.Min(255, float64(f.Pix[i])/a)), uint8(math.Min(255, float64(f.Pix[i+1])/a)), uint8(math.Min(255, float64(f.Pix[i+2])/a))
			key := uint16(r>>3)<<10 | uint16(g>>3)<<5 | uint16(b>>3)
			if h := hist[key]; h != nil {
				h.n++
			} else {
				hist[key] = &bin{r, g, b, 1}
			}
		}
	}
	boxes := [][]*bin{nil}
	for _, h := range hist {
		boxes[0] = append(boxes[0], h)
	}
	sort.Slice(boxes[0], func(i, j int) bool { // deterministic
		a, b := boxes[0][i], boxes[0][j]
		return uint32(a.r)<<16|uint32(a.g)<<8|uint32(a.b) < uint32(b.r)<<16|uint32(b.g)<<8|uint32(b.b)
	})
	span := func(bx []*bin) (axis int, rng int) {
		lo, hi := [3]int{255, 255, 255}, [3]int{}
		for _, c := range bx {
			v := [3]int{int(c.r), int(c.g), int(c.b)}
			for i := range v {
				lo[i], hi[i] = min(lo[i], v[i]), max(hi[i], v[i])
			}
		}
		for i := range lo {
			if hi[i]-lo[i] >= rng {
				axis, rng = i, hi[i]-lo[i]
			}
		}
		return
	}
	for len(boxes) < 255 {
		best, bestRng := -1, 0
		for i, bx := range boxes {
			if len(bx) < 2 {
				continue
			}
			if _, r := span(bx); r > bestRng {
				best, bestRng = i, r
			}
		}
		if best < 0 {
			break
		}
		bx := boxes[best]
		axis, _ := span(bx)
		sort.SliceStable(bx, func(i, j int) bool {
			v := func(c *bin) int { return [3]int{int(c.r), int(c.g), int(c.b)}[axis] }
			return v(bx[i]) < v(bx[j])
		})
		total := 0
		for _, c := range bx {
			total += c.n
		}
		acc, cut := 0, 1
		for i, c := range bx {
			acc += c.n
			if acc*2 >= total {
				cut = i + 1
				break
			}
		}
		if cut >= len(bx) {
			cut = len(bx) - 1
		}
		boxes[best] = bx[:cut]
		boxes = append(boxes, bx[cut:])
	}
	pal := color.Palette{color.NRGBA{}}
	for _, bx := range boxes {
		if len(bx) == 0 {
			continue
		}
		var r, g, b, n int
		for _, c := range bx {
			r += int(c.r) * c.n
			g += int(c.g) * c.n
			b += int(c.b) * c.n
			n += c.n
		}
		pal = append(pal, color.NRGBA{uint8(r / n), uint8(g / n), uint8(b / n), 255})
	}
	return pal
}

// ElementBounds is the bounding box, in the document's root user space, of
// what the element and its descendants draw, from the keys oksvg gives its
// shapes. ok is false when they draw nothing.
func ElementBounds(shapes []oksvg.Shape, key int) (x, y, w, h float64, ok bool) {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, sh := range shapes {
		in := false
		for _, k := range sh.Keys {
			if k == key {
				in = true
				break
			}
		}
		if !in {
			continue
		}
		for _, op := range sh.Path {
			n := map[byte]int{'M': 1, 'L': 1, 'Q': 2, 'C': 3}[op.Op]
			for _, p := range op.Pts[:n] {
				minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
				minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
			}
		}
	}
	if math.IsInf(minX, 1) {
		return 0, 0, 0, 0, false
	}
	return minX, minY, maxX - minX, maxY - minY, true
}
