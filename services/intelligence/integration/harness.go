package integration

import (
	"net"
	"os"
	"strconv"
	"testing"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/JaimeStill/spike-messaging/messaging/nats/natstest"
	"github.com/JaimeStill/spike-messaging/services/intelligence/internal/pgtest"
)

// The compose stack's defaults, the values config.local.json pairs with.
// The harness reads the same INTELLIGENCE_DATABASE_* and INTELLIGENCE_NATS_URL
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
	// Database is the scratch database the process migrates and runs on, and
	// Stream and Prefix the scratch JetStream stream it provisions and
	// publishes on: its own, so concurrent processes and runs never share an
	// exercise or an event. The harness drops both when the test ends.
	Database, Stream, Prefix string
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
	s := &Service{
		addr:     addr,
		client:   webtest.NewClient("http://" + addr),
		Database: pgtest.Scratch(t),
	}
	s.Stream, s.Prefix = natstest.Stream(t, NATSURL())
	s.Process = processtest.Launch(t, environment(opts, addr, s)...)
	return s
}

// NATSURL is the NATS server the service's broker connects to: the parent's
// INTELLIGENCE_NATS_URL, else the compose stack's.
func NATSURL() string {
	return getenv("INTELLIGENCE_NATS_URL", defaultNATSURL)
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

// environment composes the service's own variables for the run. INTELLIGENCE_ENV is
// cleared so no overlay applies: the base file and these variables are the
// whole configuration. The port is the one Launch reserved, the database
// server and NATS are the parent's or the compose stack's, and the database
// and the stream are the process's scratch ones.
func environment(opts Options, addr string, s *Service) []string {
	host, port, _ := net.SplitHostPort(addr)
	env := []string{
		"INTELLIGENCE_ENV=",
		"INTELLIGENCE_LOG_LEVEL=debug",
		"INTELLIGENCE_LOG_FORMAT=text",
		"INTELLIGENCE_SERVER_HOST=" + host,
		"INTELLIGENCE_SERVER_PORT=" + port,
		"INTELLIGENCE_DATABASE_HOST=" + getenv("INTELLIGENCE_DATABASE_HOST", defaultDatabaseHost),
		"INTELLIGENCE_DATABASE_PORT=" + getenv("INTELLIGENCE_DATABASE_PORT", defaultDatabasePort),
		"INTELLIGENCE_DATABASE_PASSWORD=" + getenv("INTELLIGENCE_DATABASE_PASSWORD", defaultDatabasePassword),
		"INTELLIGENCE_DATABASE_NAME=" + s.Database,
		"INTELLIGENCE_NATS_URL=" + NATSURL(),
		"INTELLIGENCE_NATS_STREAM=" + s.Stream,
		"INTELLIGENCE_NATS_PREFIX=" + s.Prefix,
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
