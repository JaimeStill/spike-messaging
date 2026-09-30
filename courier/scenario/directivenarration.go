package scenario

// standing is each faction's last directive for each element, so the
// narration tells a new directive as what it changes.
type standing map[string]map[string]directive

// changes returns each directive of d that does not continue its element's
// last one, and records d as the faction's standing directives.
func (s standing) changes(d directives) []directive {
	out, now := directiveChanges(s[d.Faction], d.Directives)
	s[d.Faction] = now
	return out
}

// directiveChanges returns each directive of ds that does not continue its
// element's directive in was. It also returns ds keyed by element, which
// replaces was.
func directiveChanges(was map[string]directive, ds []directive) (out []directive, now map[string]directive) {
	now = make(map[string]directive, len(ds))
	for _, x := range ds {
		now[x.Element] = x
		if prev, ok := was[x.Element]; ok && sameDirective(prev, x) {
			continue
		}
		out = append(out, x)
	}
	return out, now
}

// sameDirective reports whether b continues a: it has the same rule, contact,
// and target. An engage that follows its contact to another cell also
// continues it.
func sameDirective(a, b directive) bool {
	if a.Rule != b.Rule || a.Contact != b.Contact || (a.Target == nil) != (b.Target == nil) {
		return false
	}
	return a.Target == nil || a.Rule == "engage" && a.Contact != "" || *a.Target == *b.Target
}

// act renders what a directive has its squad do: a verb, the cell it heads
// for ("-> a:5,5") or fights in ("@ a:5,5"), and the contact it engages,
// each empty when the directive has none. The narration reads command's
// secure rule as a capture, and an engage in the squad's own cell as a fight
// in place; cell gives a squad's cell, and is nil when unknown. A directive
// without a rule, as courier's stand-in issues, heads for its target, or
// holds when it has none. name renders a cell.
func act(x directive, cell func(string) (location, bool), name func(location) string) (verb, target, contact string) {
	if x.Target == nil {
		return "hold", "", ""
	}
	to, at := "-> "+name(*x.Target), "@ "+name(*x.Target)
	switch {
	case x.Rule == "retreat":
		return "retreat", to, ""
	case x.Rule == "reinforce":
		return "reinforce", to, ""
	case x.Rule == "pursue":
		return "pursue", at, x.Contact
	case x.Rule == "engage" && ownCell(x, cell):
		return "fight", at, x.Contact
	case x.Rule == "engage":
		return "engage", to, x.Contact
	case x.Rule == "secure":
		return "capture", to, ""
	case x.Rule == "search":
		return "search", to, ""
	case x.Rule == "rescout":
		return "rescout", to, ""
	}
	return "head", to, ""
}

// ownCell reports whether x targets the cell its element stands in.
func ownCell(x directive, cell func(string) (location, bool)) bool {
	if cell == nil {
		return false
	}
	at, ok := cell(x.Element)
	return ok && at == *x.Target
}
