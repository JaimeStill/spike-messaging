package messaging_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/inbox"
	"github.com/JaimeStill/spike-messaging/messaging/memory"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
)

const failsafe = 2 * time.Second

// engines compiles the outbox's and the inbox's statements over sqlate's
// test dialect, so the runtime is built without a driver.
func engines(t *testing.T) (outbox.Engine, inbox.Engine) {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, text := range map[string]string{
		"emit":           "INSERT INTO o (s, id, h, d) VALUES ({{source}}, {{id}}, {{header}}, {{data}})",
		"claim_row":      "SELECT seq, header, data FROM o",
		"mark_published": "UPDATE o SET p = 1 WHERE seq = {{seq}}",
		"claim":          "INSERT INTO i (c, s, id) VALUES ({{consumer}}, {{source}}, {{id}})",
	} {
		fsys["s/"+name+".sql"] = &fstest.MapFile{Data: []byte("--| tier: standard\n" + text)}
	}
	s := query.MustCatalog(query.Patterns()).MustCompile(fsys, "s", sqltest.Dialect{})
	return outbox.Engine{
		Emit:          s.Statement("emit"),
		ClaimRow:      s.Statement("claim_row"),
		MarkPublished: s.Statement("mark_published"),
	}, inbox.Engine{Claim: s.Statement("claim")}
}

func TestNewRequiresTheEngines(t *testing.T) {
	ob, in := engines(t)
	cfg := messaging.Config{Source: "/test"}
	if _, err := messaging.New(cfg, memory.New(), outbox.Engine{}, in, slog.Default()); err == nil {
		t.Error("New accepted an empty outbox engine")
	}
	if _, err := messaging.New(cfg, memory.New(), ob, inbox.Engine{}, slog.Default()); err == nil {
		t.Error("New accepted an empty inbox engine")
	}
	rt, err := messaging.New(cfg, memory.New(), ob, in, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if rt.Outbox == nil || rt.Inbox == nil || rt.Recorder == nil || rt.Broker == nil {
		t.Errorf("New left a field unset: %+v", rt)
	}
}

type payload struct {
	N int `json:"n"`
}

// Consume decodes each event's data into the consumer's type and hands it
// over with a claim; an event whose data does not decode never reaches the
// consumer and is not redelivered, and neither is one the consumer refuses
// permanently, so the next event still arrives. Each delivery is logged
// once with its outcome: a refusal at warn with its error, a failure the
// broker redelivers as retried, and a success as handled.
func TestConsume(t *testing.T) {
	ob, in := engines(t)
	b := memory.New()
	var log syncBuffer
	rt, err := messaging.New(messaging.Config{Source: "/test"}, b, ob, in, slog.New(slog.NewTextHandler(&log, nil)))
	if err != nil {
		t.Fatal(err)
	}
	type got struct {
		data  payload
		claim bool
	}
	calls := make(chan got, 4)
	var failed sync.Once
	sub := messaging.Subscription{Name: "consumer", Types: []string{"t"}, RetryDelay: 10 * time.Millisecond}
	r, err := rt.Consume(sub, time.Second, func(_ context.Context, d payload, claim messaging.Claim) error {
		if d.N == 9 {
			return event.Permanent(errors.New("nine is refused"))
		}
		retry := false
		if d.N == 5 {
			failed.Do(func() { retry = true })
		}
		if retry {
			return errors.New("five fails once")
		}
		calls <- got{data: d, claim: claim != nil}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Shutdown(context.Background()) })

	for _, e := range []event.Event{
		{ID: "bad", Source: "/test", Type: "t", DataContentType: "application/json", Data: []byte("{")},
		{ID: "refused", Source: "/test", Type: "t", DataContentType: "application/json", Data: []byte(`{"n":9}`)},
		{ID: "flaky", Source: "/test", Type: "t", DataContentType: "application/json", Data: []byte(`{"n":5}`)},
		{ID: "good", Source: "/test", Type: "t", Subject: "s1", DataContentType: "application/json", Data: []byte(`{"n":7}`)},
	} {
		if err := b.Publish(t.Context(), e); err != nil {
			t.Fatal(err)
		}
	}
	handled := map[int]bool{}
	for len(handled) < 2 {
		select {
		case c := <-calls:
			if handled[c.data.N] || c.data.N != 5 && c.data.N != 7 || !c.claim {
				t.Fatalf("consumer got %+v, want n=5 and n=7 once each, with a claim", c)
			}
			handled[c.data.N] = true
		case <-time.After(failsafe):
			t.Fatal("timed out waiting for the good events")
		}
	}
	select {
	case c := <-calls:
		t.Errorf("consumer called again with %+v", c)
	case <-time.After(50 * time.Millisecond):
	}

	lines := strings.Split(strings.TrimSpace(log.String()), "\n")
	outcomes := func(id string) []string {
		var out []string
		for _, l := range lines {
			if strings.Contains(l, "event="+id+" ") {
				_, o, _ := strings.Cut(l, "outcome=")
				o, _, _ = strings.Cut(o, " ")
				out = append(out, o)
			}
		}
		return out
	}
	for id, want := range map[string][]string{
		"bad": {"refused"}, "refused": {"refused"}, "flaky": {"retried", "handled"}, "good": {"handled"},
	} {
		if got := outcomes(id); !slices.Equal(got, want) {
			t.Errorf("%s's deliveries were logged as %v, want %v:\n%s", id, got, want, log.String())
		}
	}
	for _, want := range []string{
		`level=WARN msg="event refused" consumer=consumer type=t event=refused`,
		`error="permanent: nine is refused"`,
		`msg="event consumed" consumer=consumer type=t event=flaky subject="" outcome=retried error="five fails once"`,
		`level=INFO msg="event consumed" consumer=consumer type=t event=good subject=s1 outcome=handled`,
	} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("the log lacks %s:\n%s", want, log.String())
		}
	}

	if _, err := rt.Consume(messaging.Subscription{Name: "a.b"}, time.Second, func(context.Context, payload, messaging.Claim) error { return nil }); err == nil {
		t.Error("Consume accepted an invalid subscription")
	}
}

func TestConfigFinalize(t *testing.T) {
	c := messaging.Config{Source: "/file"}
	c.Merge(&messaging.Config{RelayPoll: libconfig.Duration(time.Second)})
	t.Setenv("SVC_MESSAGING_SOURCE", "/env")
	if err := c.Finalize("svc"); err != nil {
		t.Fatal(err)
	}
	if c.Source != "/env" || c.RelayPoll.Duration() != time.Second {
		t.Errorf("Finalize = %+v", c)
	}

	var empty messaging.Config
	if err := empty.Finalize(""); err == nil {
		t.Error("Finalize accepted a block with no source")
	}
	if empty.RelayPoll.Duration() != messaging.DefaultRelayPoll {
		t.Errorf("RelayPoll = %v, want the default", empty.RelayPoll)
	}
}

// syncBuffer serializes the reactor's writes and the test's read.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
