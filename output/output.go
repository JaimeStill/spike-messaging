package output

import (
	"fmt"
	"io"
	"sync"
)

// Output writes results to one stream and errors to another. It is safe for
// concurrent use, so reactor handlers can narrate from their own goroutines
// without interleaving lines.
type Output struct {
	mu       sync.Mutex
	out, err io.Writer
}

// New returns an Output over stdout and stderr.
func New(stdout, stderr io.Writer) *Output {
	return &Output{out: stdout, err: stderr}
}

// Printf writes one line to stdout.
func (o *Output) Printf(format string, args ...any) {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, _ = fmt.Fprintf(o.out, format+"\n", args...)
}

// Error writes the error a command ended with to stderr.
func (o *Output) Error(err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, _ = fmt.Fprintf(o.err, "error: %v\n", err)
}
