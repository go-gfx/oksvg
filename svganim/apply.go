package svganim

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Apply writes the document as it is at time t: the attributes and transforms
// the animations give each element set on it, and the animations themselves
// taken out (an animation element becomes an empty g, a style sheet loses its
// @keyframes), so that a renderer with no animation shows the drawing at t.
// Every element keeps its key: an animation element is replaced, never
// removed, so that the keys of the result are the keys of the document.
func (a *Animations) Apply(svg []byte, t float64, bbox BBoxFunc) ([]byte, error) {
	if len(svg) > a.lim.MaxInput {
		return nil, fmt.Errorf("%w: %d bytes of SVG", ErrTooLarge, len(svg))
	}
	states := a.Eval(t, bbox)
	dec := xml.NewDecoder(bytes.NewReader(svg))
	dec.Strict = true
	var out bytes.Buffer
	key := -1
	var skip []string // names of the open elements being replaced
	inStyle := 0
	var stack []string
	for {
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch tk := tok.(type) {
		case xml.StartElement:
			key++
			name := rawName(tk.Name)
			stack = append(stack, tk.Name.Local)
			if tk.Name.Local == "style" {
				inStyle++
			}
			if IsAnimationElement(tk.Name.Local) {
				out.WriteString("<g>")
				skip = append(skip, name)
				continue
			}
			if len(skip) > 0 {
				// inside an animation element (an mpath): replaced as well
				out.WriteString("<g>")
				skip = append(skip, name)
				continue
			}
			attrs := tk.Attr
			if s := states[key]; s != nil {
				attrs = overrideAttrs(attrs, s)
			}
			out.WriteString("<" + name)
			for _, at := range attrs {
				out.WriteString(" " + rawName(at.Name) + `="`)
				if err := xml.EscapeText(&out, []byte(at.Value)); err != nil {
					return nil, err
				}
				out.WriteString(`"`)
			}
			out.WriteString(">")
		case xml.EndElement:
			if len(stack) > 0 {
				if stack[len(stack)-1] == "style" {
					inStyle--
				}
				stack = stack[:len(stack)-1]
			}
			if len(skip) > 0 {
				skip = skip[:len(skip)-1]
				out.WriteString("</g>")
				continue
			}
			out.WriteString("</" + rawName(tk.Name) + ">")
		case xml.CharData:
			if len(skip) > 0 {
				continue
			}
			text := string(tk)
			if inStyle > 0 {
				text = stripKeyframes(text)
			}
			if err := xml.EscapeText(&out, []byte(text)); err != nil {
				return nil, err
			}
		}
	}
	return out.Bytes(), nil
}

func rawName(n xml.Name) string {
	if n.Space != "" {
		return n.Space + ":" + n.Local
	}
	return n.Local
}

// overrideAttrs sets the animated attributes on a copy of the element's, and
// takes the same properties out of its style attribute, where they would be
// read before the attributes.
func overrideAttrs(attrs []xml.Attr, s *State) []xml.Attr {
	out := make([]xml.Attr, 0, len(attrs)+len(s.Attrs)+1)
	set := map[string]string{}
	for k, v := range s.Attrs {
		set[k] = v.String()
	}
	if s.Transform != nil {
		set["transform"] = s.Transform.String()
	}
	for _, at := range attrs {
		if _, ok := set[at.Name.Local]; ok && at.Name.Space == "" {
			continue
		}
		if at.Name.Local == "style" && at.Name.Space == "" {
			at.Value = withoutProps(at.Value, set)
		}
		out = append(out, at)
	}
	names := make([]string, 0, len(set))
	for k := range set {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		out = append(out, xml.Attr{Name: xml.Name{Local: k}, Value: set[k]})
	}
	return out
}

func withoutProps(style string, props map[string]string) string {
	var keep []string
	for _, d := range splitTop(style, ';') {
		k, _, ok := strings.Cut(d, ":")
		if ok {
			if _, drop := props[strings.TrimSpace(k)]; drop {
				continue
			}
		}
		if strings.TrimSpace(d) != "" {
			keep = append(keep, d)
		}
	}
	return strings.Join(keep, ";")
}

// stripKeyframes removes the @keyframes blocks of a style sheet.
func stripKeyframes(css string) string {
	var b strings.Builder
	for {
		i := strings.Index(css, "@")
		if i < 0 {
			b.WriteString(css)
			return b.String()
		}
		rest := css[i:]
		open := strings.IndexByte(rest, '{')
		head := ""
		if open >= 0 {
			head = rest[:open]
		}
		if open < 0 || !strings.Contains(head, "keyframes") {
			b.WriteString(css[:i+1])
			css = css[i+1:]
			continue
		}
		b.WriteString(css[:i])
		_, after, ok := block(rest[open+1:])
		if !ok {
			return b.String()
		}
		css = after
	}
}
