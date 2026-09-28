package integration

import (
	"net"
	"strconv"
	"testing"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk/webtest"
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
	s := &Service{addr: addr, client: webtest.NewClient("http://" + addr)}
	s.Process = processtest.Launch(t, environment(opts, addr)...)
	return s
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
// whole configuration. The port is the one Launch reserved.
func environment(opts Options, addr string) []string {
	host, port, _ := net.SplitHostPort(addr)
	env := []string{
		"EXERCISE_ENV=",
		"EXERCISE_LOG_LEVEL=debug",
		"EXERCISE_LOG_FORMAT=text",
		"EXERCISE_SERVER_HOST=" + host,
		"EXERCISE_SERVER_PORT=" + port,
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
