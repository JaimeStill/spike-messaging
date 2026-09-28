package scenario

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/pflag"

	"github.com/JaimeStill/spike-messaging/core/reactor"
)

// Request is one request a responder receives: its body, and Respond, which
// sends the reply to whoever asked.
type Request struct {
	Body    []byte
	Respond func(body []byte) error
}

// Exchange is a native request and reply exchange on one address, ready for
// one run. It sits outside the broker's standard tier, so the composition
// root builds it on the provider's own client.
type Exchange struct {
	Name string // the address requests go to, for the narration
	// Serve receives the requests sent to the address. It runs as a reactor
	// on the scenario's coordinator, so it drains with the others.
	Serve reactor.Source[Request]
	// Ask sends one request and waits for its reply, until ctx ends.
	Ask func(ctx context.Context, body []byte) ([]byte, error)
}

// Exchanges builds a fresh exchange for one scenario run, with the release
// that frees what it holds, such as a connection. A scenario calls the
// release from its cleanup, once its coordinator has drained. The release
// may be nil.
type Exchanges func() (ex Exchange, release func() error, err error)

// requestScenario runs a responder reactor on the coordinator and sends it
// requests through the exchange, each waiting for its reply: evidence 8, a
// native use kept out of this package.
func requestScenario(exchanges Exchanges, needs func() []Need) Scenario {
	requests := 5
	return Scenario{
		Name:    "request",
		Summary: "A responder reactor answers requests sent through the provider's native request and reply",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			fs.IntVar(&requests, "requests", requests, "how many requests are sent, one at a time")
		},
		Validate: func() error {
			if requests < 1 {
				return errors.New("--requests must be at least 1")
			}
			return nil
		},
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(defaultDrain)
			var l lease
			var ex Exchange
			var replies []string
			return []Step{
				{
					Intent: "Register a responder on an address of its own at stage 0, and start the coordinator",
					Action: func(ctx context.Context, rep *Reporter) error {
						if exchanges == nil {
							return errors.New("no request exchange is configured")
						}
						var err error
						if ex, l.release, err = exchanges(); err != nil {
							return err
						}
						handle := func(_ context.Context, req Request) error {
							rep.Note("responder received %q", req.Body)
							return req.Respond(reply(req.Body))
						}
						r := reactor.New(ex.Serve, handle, reactor.Grace(defaultGrace))
						c.add("responder", 0, r)
						if err := c.start(ctx, rep); err != nil {
							return err
						}
						if err := c.awaitReady(ctx, r, "the responder"); err != nil {
							return err
						}
						rep.Note("responder receiving on %s", ex.Name)
						return nil
					},
				},
				{
					Intent: fmt.Sprintf("Send %d requests, each waiting for its reply", requests),
					Action: func(ctx context.Context, rep *Reporter) error {
						for n := 1; n <= requests; n++ {
							body := fmt.Sprintf("request %d", n)
							rep.Note("asking %q", body)
							actx, cancel := context.WithTimeout(ctx, patience)
							got, err := ex.Ask(actx, []byte(body))
							cancel()
							if err != nil {
								return fmt.Errorf("ask %q: %w", body, err)
							}
							rep.Note("reply %q", got)
							replies = append(replies, string(got))
						}
						return nil
					},
				},
				{
					Intent: "Check that every request was answered by its own reply",
					Action: func(_ context.Context, rep *Reporter) error {
						var errs []error
						for i, got := range replies {
							if want := string(reply(fmt.Appendf(nil, "request %d", i+1))); got != want {
								errs = append(errs, fmt.Errorf("request %d: reply %q, want %q", i+1, got, want))
							}
						}
						if len(replies) != requests {
							errs = append(errs, fmt.Errorf("%d replies, want %d", len(replies), requests))
						}
						if len(errs) == 0 {
							rep.Note("each of %d requests answered by its own reply", requests)
						}
						return errors.Join(errs...)
					},
				},
				{
					Intent: "Signal the drain: the responder stops receiving",
					Action: func(_ context.Context, rep *Reporter) error { return c.stop(rep) },
				},
			}, afterDrain(c, &l)
		},
	}
}

// reply is the responder's answer to body.
func reply(body []byte) []byte { return fmt.Appendf(nil, "reply to %s", body) }
