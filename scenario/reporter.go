package scenario

import "github.com/JaimeStill/spike-messaging/output"

// Reporter narrates a scenario through the Output every courier command
// renders with.
type Reporter struct {
	out *output.Output
}

// NewReporter returns a Reporter writing through out.
func NewReporter(out *output.Output) *Reporter {
	return &Reporter{out: out}
}

// Intent prints step i of n's intent as a heading, set off by a blank line.
func (r *Reporter) Intent(i, n int, intent string) {
	r.out.Printf("")
	r.out.Printf("[%d/%d] %s", i, n, intent)
}

// Note prints one indented line of what the step observed. It is safe to
// call from a reactor's handler.
func (r *Reporter) Note(format string, args ...any) {
	r.out.Printf("  "+format, args...)
}
