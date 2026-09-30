package scenario

import (
	"fmt"
	"strings"
)

// part is one cell of a line in a list. A cell aligns with the cells of
// the same key in the same position of the other lines of its list: it is
// padded to the widest of them, on its right, or on its left when it is
// right set, as a number is. The last cell of a line is not padded on its
// right. A cell is set off from the one before it by a space, or by two
// when it is apart.
type part struct {
	key, text    string
	apart, right bool
}

// col is a cell of text in the column key.
func col(key, text string) part { return part{key: key, text: text} }

// apart is col set off by two spaces.
func apart(key, text string) part { return part{key: key, text: text, apart: true} }

// detail is one line of a list, with the list under it.
type detail struct {
	cells []part
	sub   []detail
}

// row is a detail of cells.
func row(cells ...part) detail { return detail{cells: cells} }

// text is a detail of one line of text, aligned with nothing.
func text(s string) detail { return row(col("", s)) }

// with returns d with sub listed under it.
func (d detail) with(sub ...detail) detail {
	d.sub = append(d.sub, sub...)
	return d
}

// list renders ds one line each, at indent, and the list under each two
// spaces deeper, its cells aligned among themselves. name renders each
// cell's text first.
func list(ds []detail, indent string, name func(string) string) []string {
	type column struct {
		at  int
		key string
	}
	width := map[column]int{}
	for _, d := range ds {
		for i, c := range d.cells {
			k := column{i, c.key}
			width[k] = max(width[k], len(name(c.text)))
		}
	}
	var out []string
	for _, d := range ds {
		var b strings.Builder
		b.WriteString(indent)
		for i, c := range d.cells {
			t := name(c.text)
			pad := strings.Repeat(" ", width[column{i, c.key}]-len(t))
			switch {
			case i == 0:
			case c.apart:
				b.WriteString("  ")
			default:
				b.WriteString(" ")
			}
			switch {
			case c.right:
				b.WriteString(pad + t)
			case i < len(d.cells)-1:
				b.WriteString(t + pad)
			default:
				b.WriteString(t)
			}
		}
		out = append(out, b.String())
		out = append(out, list(d.sub, indent+"  ", name)...)
	}
	return out
}

// same renders a cell's text as it is.
func same(s string) string { return s }

// labeled is a row of items under a label, as a round's block lists them.
type labeled struct {
	label string
	items []string
	each  bool // one item to a line, however few
}

// wrapAt is the width past which a row lists one item to a line.
const wrapAt = 100

// lines renders a row at indent: its label padded to width, then its
// items, joined by " · " on one line, or one to a line aligned under the
// first when the row asks for it or one line would pass wrapAt. name
// renders each item first.
func (l labeled) lines(indent string, width int, name func(string) string) []string {
	items := make([]string, len(l.items))
	for i, it := range l.items {
		items[i] = name(it)
	}
	head := fmt.Sprintf("%s%-*s  ", indent, width, l.label)
	if one := head + strings.Join(items, " · "); !l.each && len(one) <= wrapAt || len(items) == 0 {
		return []string{strings.TrimRight(one, " ")}
	}
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = strings.Repeat(" ", len(head)) + it
	}
	out[0] = head + items[0]
	return out
}
