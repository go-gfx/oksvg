package oksvg

import (
	"strings"
	"testing"
)

// An element's key is its position among the document's elements; the paths
// and shapes it draws carry the keys of it and its ancestors.
func TestPathsCarryElementKeys(t *testing.T) {
	icon, err := ReadIconStream(strings.NewReader(`<svg viewBox="0 0 10 10">
<defs><rect id="d" width="1" height="1"/></defs>
<g id="g"><rect width="4" height="4"/><circle cx="5" cy="5" r="2" fill="red"/></g>
<use href="#d"/><path d="M0 0L9 9L0 9Z"/></svg>`), IgnoreErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	// svg 0, defs 1, rect#d 2, g 3, rect 4, circle 5, use 6, path 7
	var got [][]int
	for _, p := range icon.SVGPaths {
		got = append(got, p.Keys)
	}
	want := [][]int{{0, 3, 4}, {0, 3, 5}, {0, 6}, {0, 7}}
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("keys = %v, want %v", got, want)
		}
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Fatalf("keys = %v, want %v", got, want)
			}
		}
	}
	shapes, _ := icon.Shapes()
	if len(shapes) == 0 || len(shapes[0].Keys) != 3 {
		t.Fatalf("shapes do not carry the keys: %+v", shapes)
	}
}
