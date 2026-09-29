package messaging_test

import (
	"bytes"
	"context"
	"log/slog"
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
// consumer and is not redelivered, so the next event still arrives, and
// its refusal is logged once.
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
	sub := messaging.Subscription{Name: "consumer", Types: []string{"t"}}
	r, err := rt.Consume(sub, time.Second, func(_ context.Context, d payload, claim messaging.Claim) error {
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
		{ID: "good", Source: "/test", Type: "t", DataContentType: "application/json", Data: []byte(`{"n":7}`)},
	} {
		if err := b.Publish(t.Context(), e); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case c := <-calls:
		if c.data.N != 7 || !c.claim {
			t.Errorf("consumer got %+v, want n=7 with a claim", c)
		}
	case <-time.After(failsafe):
		t.Fatal("timed out waiting for the good event")
	}
	select {
	case c := <-calls:
		t.Errorf("consumer called again with %+v", c)
	case <-time.After(50 * time.Millisecond):
	}

	if n := strings.Count(log.String(), "event=bad"); n != 1 || !strings.Contains(log.String(), "consumer=consumer") {
		t.Errorf("the refusal was logged %d times, want once with its consumer:\n%s", n, log.String())
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
