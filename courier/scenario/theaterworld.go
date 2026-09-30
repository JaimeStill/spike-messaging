package scenario

// world is the exercise as the observer knows it: the start, what the
// narration reads of the observer's view at the start (the seed, and the
// rounds a capture takes), the objectives, and who holds each.
type world struct {
	setup         *started            // the start, once it is in
	seed          int64               // the exercise's seed, from the view
	captureRounds int                 // the rounds a faction must hold an objective alone to take it, from the view's rules
	sites         []location          // the objectives, as the observer's view has them
	holders       map[location]string // each objective's holder, as the captures and the loss alerts tell it
	resolves      int                 // the last round resolved
}

// learn takes the seed, the rules, and the objectives from the observer's
// view, read at the start, when no objective is held. handle validates the
// view's rules before it learns them.
func (w *world) learn(v exerciseView) {
	w.seed = v.Seed
	w.captureRounds = v.Rules.CaptureRounds
	w.sites = v.State.objectives()
	for _, l := range w.sites {
		w.holders[l] = ""
	}
}

// capture records holder as the holder of the objective at.
func (w *world) capture(at location, holder string) { w.holders[at] = holder }

// name renders a cell for the narration: an objective's as objective:x,y,
// any other as sector:x,y. A cell is an objective's when the holders hold
// it, as the view, a capture, or a loss alert tells it.
func (w *world) name(l location) string {
	if _, ok := w.holders[l]; ok {
		return objectiveName(l)
	}
	return place(l)
}

// factions returns the exercise's factions, or none before the start.
func (w *world) factions() []string {
	if w.setup == nil {
		return nil
	}
	return w.setup.Factions
}
