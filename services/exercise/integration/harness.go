package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk/webtest"
)

// The compose stack's defaults, the values config.local.json pairs with.
// The harness reads the same EXERCISE_DATABASE_* and EXERCISE_MESSAGING_URL
// variables first, so a run against another stack sets them.
const (
	defaultDatabaseHost     = "127.0.0.1"
	defaultDatabasePort     = "5435"
	defaultDatabasePassword = "app"
	defaultNATSURL          = "nats://127.0.0.1:4225"
)

// Main is the suite's TestMain: it builds cmd/server once, with the race
// detector so the service runs under it too, runs the tests, and removes
// the build.
func Main(m *testing.M) {
	processtest.Main(m, "./cmd/server")
}

// Options shapes one service process. The zero value runs the service on
// the base configuration file and the harness's own variables alone.
type Options struct {
	// Env appends further KEY=VALUE overrides, applied last.
	Env []string
}

// Service is one running service process: its address, its captured
// output, and its exit.
type Service struct {
	*processtest.Process
	addr   string
	client *webtest.Client
	// Stream and Prefix are the scratch JetStream stream the process
	// provisions and publishes on, its own so concurrent processes and runs
	// never share events. The harness deletes the stream when the test ends.
	Stream, Prefix string
}

// Start runs the service with opts and returns once it is live: Launch
// then Ready.
func Start(t testing.TB, opts Options) *Service {
	t.Helper()
	return Launch(t, opts).Ready(t)
}

// Launch runs the service with opts on a reserved loopback port and returns
// without waiting, so a test can start several processes at once; Ready
// waits for one.
func Launch(t testing.TB, opts Options) *Service {
	t.Helper()
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(processtest.FreePort(t)))
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	tag := hex.EncodeToString(b)
	s := &Service{
		addr:   addr,
		client: webtest.NewClient("http://" + addr),
		Stream: "test_" + tag,
		Prefix: "test." + tag,
	}
	t.Cleanup(func() { deleteStream(t, s.Stream) })
	s.Process = processtest.Launch(t, environment(opts, addr, s.Stream, s.Prefix)...)
	return s
}

// NATSURL is the NATS server the service's broker connects to: the parent's
// EXERCISE_MESSAGING_URL, else the compose stack's.
func NATSURL() string {
	return getenv("EXERCISE_MESSAGING_URL", defaultNATSURL)
}

// deleteStream removes a process's scratch stream, if it provisioned one.
func deleteStream(t testing.TB, stream string) {
	nc, err := natsgo.Connect(NATSURL())
	if err != nil {
		t.Logf("delete stream %s: %v", stream, err)
		return
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Logf("delete stream %s: %v", stream, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = js.DeleteStream(ctx, stream)
}

func getenv(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// Ready waits until the service's liveness probe answers, failing the test
// with the captured output if the process exits or the failsafe elapses
// first. The server is the root lifecycle stage, so a live probe means
// every stage beneath it started.
func (s *Service) Ready(t testing.TB) *Service {
	t.Helper()
	s.Await(t, "liveness", func() bool { return webtest.Live(s.URL()) })
	return s
}

// environment composes the service's own variables for the run. EXERCISE_ENV is
// cleared so no overlay applies: the base file and these variables are the
// whole configuration. The port is the one Launch reserved, the database
// and NATS are the parent's or the compose stack's, and the stream is the
// process's scratch stream.
func environment(opts Options, addr, stream, prefix string) []string {
	host, port, _ := net.SplitHostPort(addr)
	env := []string{
		"EXERCISE_ENV=",
		"EXERCISE_LOG_LEVEL=debug",
		"EXERCISE_LOG_FORMAT=text",
		"EXERCISE_SERVER_HOST=" + host,
		"EXERCISE_SERVER_PORT=" + port,
		"EXERCISE_DATABASE_HOST=" + getenv("EXERCISE_DATABASE_HOST", defaultDatabaseHost),
		"EXERCISE_DATABASE_PORT=" + getenv("EXERCISE_DATABASE_PORT", defaultDatabasePort),
		"EXERCISE_DATABASE_PASSWORD=" + getenv("EXERCISE_DATABASE_PASSWORD", defaultDatabasePassword),
		"EXERCISE_MESSAGING_URL=" + NATSURL(),
		"EXERCISE_MESSAGING_STREAM=" + stream,
		"EXERCISE_MESSAGING_PREFIX=" + prefix,
	}
	return append(env, opts.Env...)
}

// Addr is the service's address, host:port.
func (s *Service) Addr() string { return s.addr }

// URL is the service's base URL.
func (s *Service) URL() string { return "http://" + s.addr }

// Client returns the client bound to the service, one per process so its
// connection is reused across calls.
func (s *Service) Client() *webtest.Client { return s.client }
