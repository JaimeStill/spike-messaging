//go:build integration

package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"slices"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// Two runs join one stream, as a directives run beside an assessments run
// does. The first run's release deletes its own consumer and leaves the
// second's, which keeps its place on the stream.
func TestJoinReleasesOnlyItsOwnConsumers(t *testing.T) {
	url := os.Getenv("MESSAGING_NATS_URL")
	if url == "" {
		t.Fatal("MESSAGING_NATS_URL is not set; run under mise with the compose stack up")
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	stream, prefix := "jointest_"+hex.EncodeToString(b), "jointest"+hex.EncodeToString(b)
	infra := newInfrastructure(&Config{Broker: "nats", NATSURL: url})

	nc, err := natsgo.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = js.DeleteStream(ctx, stream)
	})

	join := func(name string) func() error {
		t.Helper()
		b, release, err := infra.Join(stream, prefix, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		src, err := b.Subscribe(messaging.Subscription{Name: name, Types: []string{"test.event"}})
		if err != nil {
			t.Fatal(err)
		}
		// The source binds its consumer once it receives, and the run stops
		// receiving before its release, as a scenario's drain does.
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			done <- src.Receive(ctx, func(context.Context, event.Event) error { return nil })
		}()
		deadline := time.Now().Add(10 * time.Second)
		for !src.Ready() {
			if time.Now().After(deadline) {
				t.Fatalf("%s never received", name)
			}
			time.Sleep(10 * time.Millisecond)
		}
		return func() error {
			cancel()
			if err := <-done; err != nil {
				return err
			}
			return release()
		}
	}
	releaseFirst := join("courier-directives-first")
	releaseSecond := join("courier-assessments-second")

	consumers := func() []string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		s, err := js.Stream(ctx, stream)
		if err != nil {
			t.Fatal(err)
		}
		names := s.ConsumerNames(ctx)
		var out []string
		for n := range names.Name() {
			out = append(out, n)
		}
		if err := names.Err(); err != nil {
			t.Fatal(err)
		}
		slices.Sort(out)
		return out
	}

	if err := releaseFirst(); err != nil {
		t.Fatal(err)
	}
	if got := consumers(); !slices.Equal(got, []string{"courier-assessments-second"}) {
		t.Errorf("after the first release, consumers = %v, want the second run's alone", got)
	}
	if err := releaseSecond(); err != nil {
		t.Fatal(err)
	}
	if got := consumers(); len(got) != 0 {
		t.Errorf("after both releases, consumers = %v, want none", got)
	}
}
