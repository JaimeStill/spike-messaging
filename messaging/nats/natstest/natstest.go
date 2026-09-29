// Package natstest gives each test its own scratch JetStream stream, so no
// test depends on another. [Scratch] serves the provider's integration
// suite, on the server MESSAGING_NATS_URL names; [Stream] serves a service's
// harness, which points a service process at a server of its choosing. The
// stream is deleted when the test ends.
package natstest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Scratch returns the NATS server's URL, from MESSAGING_NATS_URL, and a
// stream name and subject prefix unique to the test. A missing
// MESSAGING_NATS_URL fails the test: the integration tag states that the
// stack is expected.
func Scratch(t testing.TB) (url, stream, prefix string) {
	t.Helper()
	url = os.Getenv("MESSAGING_NATS_URL")
	if url == "" {
		t.Fatal("MESSAGING_NATS_URL is not set; run under mise with the compose stack up")
	}
	stream, prefix = Stream(t, url)
	return url, stream, prefix
}

// Stream returns a stream name and subject prefix unique to the test, and
// deletes the stream on url's server when the test ends, tolerating one that
// nothing created. The stream is created by whatever broker the test points
// at it, so the cleanup registered here runs after that broker's own.
func Stream(t testing.TB, url string) (stream, prefix string) {
	t.Helper()
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("random suffix: %v", err)
	}
	suffix := hex.EncodeToString(b[:])
	stream, prefix = "test_"+suffix, "test."+suffix
	t.Cleanup(func() {
		nc, err := natsgo.Connect(url, natsgo.Name(t.Name()))
		if err != nil {
			t.Errorf("delete stream %s: connect %s: %v", stream, url, err)
			return
		}
		defer nc.Close()
		js, err := jetstream.New(nc)
		if err != nil {
			t.Errorf("delete stream %s: %v", stream, err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := js.DeleteStream(ctx, stream); err != nil && !errors.Is(err, jetstream.ErrStreamNotFound) {
			t.Errorf("delete stream %s: %v", stream, err)
		}
	})
	return stream, prefix
}
