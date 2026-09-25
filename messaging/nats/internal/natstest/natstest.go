//go:build integration

// Package natstest gives each integration-tagged test its own scratch stream
// on the NATS server MESSAGING_NATS_URL names, so no test depends on another.
// The stream is deleted when the test ends.
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

// Scratch returns a connection for the test to hand to a broker, and a
// stream name and subject prefix unique to the test. A missing
// MESSAGING_NATS_URL fails the test: the integration tag states that the
// stack is expected. The cleanup closes the test's connection, which a
// broker's Shutdown may already have done, and deletes the stream on a
// connection of its own, tolerating one no broker created.
func Scratch(t testing.TB) (nc *natsgo.Conn, stream, prefix string) {
	t.Helper()
	url := os.Getenv("MESSAGING_NATS_URL")
	if url == "" {
		t.Fatal("MESSAGING_NATS_URL is not set; run under mise with the compose stack up")
	}
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("random suffix: %v", err)
	}
	suffix := hex.EncodeToString(b[:])
	stream, prefix = "test_"+suffix, "test."+suffix

	admin := connect(t, url)
	nc = connect(t, url)
	t.Cleanup(func() {
		nc.Close()
		defer admin.Close()
		js, err := jetstream.New(admin)
		if err != nil {
			t.Errorf("jetstream: %v", err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := js.DeleteStream(ctx, stream); err != nil && !errors.Is(err, jetstream.ErrStreamNotFound) {
			t.Errorf("delete stream %s: %v", stream, err)
		}
	})
	return nc, stream, prefix
}

func connect(t testing.TB, url string) *natsgo.Conn {
	t.Helper()
	nc, err := natsgo.Connect(url, natsgo.Name(t.Name()))
	if err != nil {
		t.Fatalf("connect %s: %v", url, err)
	}
	return nc
}
