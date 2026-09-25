package scenario_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/courier/output"
	"github.com/JaimeStill/spike-messaging/courier/scenario"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/memory"
)

func memoryBrokers() (messaging.Broker, func() error, error) { return memory.New(), nil, nil }

// execute runs the named scenario's command with args on the memory broker.
func execute(t *testing.T, name string, args ...string) (string, error) {
	t.Helper()
	for _, s := range scenario.Scenarios(memoryBrokers, nil, nil, nil, fakeExchanges(nil), nil) {
		if s.Name != name {
			continue
		}
		rep, out := reporter()
		cmd := scenario.Command(s, func() *scenario.Reporter { return rep })
		cmd.SetArgs(args)
		err := cmd.ExecuteContext(t.Context())
		return out.String(), err
	}
	t.Fatalf("no scenario %q", name)
	return "", nil
}

func TestScenariosOnMemory(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"every", []string{"--interval", "20ms", "--work", "5ms"}, []string{"tick 3 done", "drained cleanly", "3 ticks handled"}},
		{"group", []string{"--events", "8", "--interval", "5ms", "--work", "20ms"}, []string{"worker-a handled", "worker-b handled", "drained cleanly"}},
		{"retry", []string{"--retry", "50ms"}, []string{"attempt 1 failed", "attempt 2 handled", "two deliveries"}},
		{"permanent", []string{"--quiet", "150ms"}, []string{"fails permanently", "event 2 handled", "delivered once and terminated"}},
		{"drain", []string{"--work", "50ms"}, []string{"handling finished", "drained cleanly", "acknowledgement held"}},
		{"request", []string{"--requests", "3"}, []string{
			"responder receiving on fake",
			`asking "request 1"`, `responder received "request 1"`, `reply "reply to request 1"`,
			`reply "reply to request 3"`,
			"each of 3 requests answered by its own reply", "drained cleanly",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := execute(t, c.name, c.args...)
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("narration lacks %q:\n%s", w, out)
				}
			}
		})
	}
}

// Each run that builds a broker releases it once, in its cleanup, and a
// failed release fails the run.
func TestBrokerReleasedOnce(t *testing.T) {
	cases := map[string][]string{
		"group":     {"--events", "4", "--interval", "5ms", "--work", "5ms"},
		"retry":     {"--retry", "20ms"},
		"permanent": {"--quiet", "100ms"},
		"drain":     {"--work", "20ms"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var released atomic.Int32
			fail := errors.New("release failed")
			brokers := func() (messaging.Broker, func() error, error) {
				return memory.New(), func() error { released.Add(1); return fail }, nil
			}
			for _, s := range scenario.Scenarios(brokers, nil, nil, nil, nil, nil) {
				if s.Name != name {
					continue
				}
				rep, out := reporter()
				cmd := scenario.Command(s, func() *scenario.Reporter { return rep })
				cmd.SetArgs(args)
				err := cmd.ExecuteContext(t.Context())
				if !errors.Is(err, fail) || !strings.Contains(err.Error(), name+": cleanup: ") {
					t.Errorf("err = %v, want the release's failure in the cleanup\n%s", err, out)
				}
				if n := released.Load(); n != 1 {
					t.Errorf("released %d times, want once", n)
				}
			}
		})
	}
}

// The request scenario releases its exchange once, in its cleanup, and a
// failed release fails the run.
func TestExchangeReleasedOnce(t *testing.T) {
	var released atomic.Int32
	fail := errors.New("release failed")
	exchanges := fakeExchanges(func() error { released.Add(1); return fail })
	for _, s := range scenario.Scenarios(memoryBrokers, nil, nil, nil, exchanges, nil) {
		if s.Name != "request" {
			continue
		}
		rep, out := reporter()
		cmd := scenario.Command(s, func() *scenario.Reporter { return rep })
		cmd.SetArgs([]string{"--requests", "2"})
		err := cmd.ExecuteContext(t.Context())
		if !errors.Is(err, fail) || !strings.Contains(err.Error(), "request: cleanup: ") {
			t.Errorf("err = %v, want the release's failure in the cleanup\n%s", err, out)
		}
		if !strings.Contains(out.String(), "drained cleanly") {
			t.Errorf("the coordinator did not drain before the release:\n%s", out)
		}
		if n := released.Load(); n != 1 {
			t.Errorf("released %d times, want once", n)
		}
	}
}

// A reply that does not match its request fails the check.
func TestRequestChecksEachReply(t *testing.T) {
	exchanges := func() (scenario.Exchange, func() error, error) {
		ex, release, err := fakeExchanges(nil)()
		ask := ex.Ask
		ex.Ask = func(ctx context.Context, body []byte) ([]byte, error) {
			got, err := ask(ctx, body)
			if string(body) == "request 2" {
				got = []byte("reply to someone else")
			}
			return got, err
		}
		return ex, release, err
	}
	for _, s := range scenario.Scenarios(memoryBrokers, nil, nil, nil, exchanges, nil) {
		if s.Name != "request" {
			continue
		}
		rep, out := reporter()
		cmd := scenario.Command(s, func() *scenario.Reporter { return rep })
		cmd.SetArgs([]string{"--requests", "3"})
		err := cmd.ExecuteContext(t.Context())
		if err == nil || !strings.Contains(err.Error(), `request 2: reply "reply to someone else", want "reply to request 2"`) {
			t.Errorf("err = %v\n%s", err, out)
		}
	}
}

func TestRequestsIsUsage(t *testing.T) {
	_, err := execute(t, "request", "--requests", "0")
	if !errors.Is(err, scenario.ErrUsage) {
		t.Errorf("err = %v, want a usage error", err)
	}
}

// chanSource is a Source of requests fed by a channel, standing in for a
// provider's native request and reply.
type chanSource struct {
	reqs  chan scenario.Request
	ready atomic.Bool
}

func (s *chanSource) Receive(ctx context.Context, fn reactor.Func[scenario.Request]) error {
	s.ready.Store(true)
	defer s.ready.Store(false)
	for {
		select {
		case <-ctx.Done():
			return nil
		case req := <-s.reqs:
			if err := fn(ctx, req); err != nil {
				return err
			}
		}
	}
}

func (s *chanSource) Ready() bool { return s.ready.Load() }

// fakeExchanges builds in-process exchanges named "fake", each released by
// release.
func fakeExchanges(release func() error) scenario.Exchanges {
	return func() (scenario.Exchange, func() error, error) {
		src := &chanSource{reqs: make(chan scenario.Request)}
		ask := func(ctx context.Context, body []byte) ([]byte, error) {
			replies := make(chan []byte, 1)
			req := scenario.Request{Body: body, Respond: func(b []byte) error { replies <- b; return nil }}
			select {
			case src.reqs <- req:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			select {
			case b := <-replies:
				return b, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return scenario.Exchange{Name: "fake", Serve: src, Ask: ask}, release, nil
	}
}

func TestEveryFailureEndsTheRun(t *testing.T) {
	out, err := execute(t, "every", "--interval", "10ms", "--work", "1ms", "--ticks", "5", "--fail-after", "2")
	if err == nil || !strings.Contains(err.Error(), "tick 2 failed") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if errors.Is(err, scenario.ErrUsage) {
		t.Error("a runtime failure is not a usage error")
	}
}

func TestDrainCancelsAfterGrace(t *testing.T) {
	out, err := execute(t, "drain", "--work", "5s", "--grace", "100ms", "--drain", "2s")
	if err == nil || !strings.Contains(err.Error(), "worker: reactor: handlers cancelled after grace 100ms") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.Contains(out, "handling cancelled") {
		t.Errorf("narration lacks the cancellation:\n%s", out)
	}
}

func TestGraceBelowDrainIsUsage(t *testing.T) {
	for _, name := range []string{"every", "drain"} {
		out, err := execute(t, name, "--grace", "5s", "--drain", "5s")
		if !errors.Is(err, scenario.ErrUsage) {
			t.Errorf("%s: err = %v, want a usage error", name, err)
		}
		if strings.Contains(out, "[1/") {
			t.Errorf("%s: a step ran before the usage error:\n%s", name, out)
		}
	}
}

// syncBuffer is a bytes.Buffer a test can read while handlers write to it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// An interrupt while a step waits drains the coordinator in the cleanup,
// which narrates the drain and reports its failure alongside the interrupt.
func TestInterruptDrainsAndReports(t *testing.T) {
	for _, s := range scenario.Scenarios(memoryBrokers, nil, nil, nil, fakeExchanges(nil), nil) {
		if s.Name != "every" {
			continue
		}
		var out syncBuffer
		rep := scenario.NewReporter(output.New(&out, &bytes.Buffer{}))
		cmd := scenario.Command(s, func() *scenario.Reporter { return rep })
		cmd.SetArgs([]string{"--interval", "10ms", "--ticks", "100", "--work", "5s", "--grace", "100ms", "--drain", "2s"})
		ctx, cancel := context.WithCancel(t.Context())
		go func() {
			for !strings.Contains(out.String(), "tick 1 started") {
				time.Sleep(time.Millisecond)
			}
			cancel()
		}()
		err := cmd.ExecuteContext(ctx)
		if !errors.Is(err, context.Canceled) || !strings.HasPrefix(err.Error(), "every: ") {
			t.Errorf("err = %v, want the interrupt, named by the scenario", err)
		}
		if err == nil || !strings.Contains(err.Error(), "handlers cancelled after grace 100ms") {
			t.Errorf("err = %v: the interrupted drain's report was lost", err)
		}
		if !strings.Contains(out.String(), "draining the coordinator") {
			t.Errorf("narration lacks the drain:\n%s", out.String())
		}
	}
}
