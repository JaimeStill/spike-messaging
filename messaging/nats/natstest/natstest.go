// Package natstest gives each test its own scratch JetStream stream, so no
// test depends on another. [Open] serves the provider's integration suite,
// on the server MESSAGING_NATS_URL names; [Scratch] serves a service's
// harness, which points a service process at a server of its choosing. The
// stream is deleted when the test ends. The names follow pgtest's: Open
// reads the environment, and Scratch takes the server.
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

// Open returns the NATS server's URL, from MESSAGING_NATS_URL, and a stream
// name and subject prefix unique to the test. A missing MESSAGING_NATS_URL
// fails the test: the integration tag states that the stack is expected.
func Open(t testing.TB) (url, stream, prefix string) {
	t.Helper()
	url = os.Getenv("MESSAGING_NATS_URL")
	if url == "" {
		t.Fatal("MESSAGING_NATS_URL is not set; run under mise with the compose stack up")
	}
	stream, prefix = Scratch(t, url)
	return url, stream, prefix
}

// Scratch returns a stream name and subject prefix unique to the test, and
// deletes the stream on url's server when the test ends, tolerating one that
// nothing created. The stream is created by whatever broker the test points
// at it, so the cleanup registered here runs after that broker's own.
func Scratch(t testing.TB, url string) (stream, prefix string) {
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
