// Package configtest builds hermetically valid service configuration for
// tests. It is the single place the suites learn what the root config
// requires: when a subsystem's block gains a required field, it is set here
// once and every consuming test adapts.
package configtest

import (
	"fmt"
	"net"
	"testing"

	"github.com/standards-lab/go-core/logging"

	"github.com/JaimeStill/spike-messaging/services/command/internal/config"
)

// Minimal returns an unfinalized Config carrying only the fields no default
// supplies: the database's name, the messaging block's source, and the nats
// block's stream and prefix.
func Minimal() *config.Config {
	cfg := &config.Config{}
	cfg.Database.Name = "command"
	cfg.Messaging.Source = "/command"
	cfg.NATS.Stream = "test"
	cfg.NATS.Prefix = "test"
	return cfg
}

// Config returns a finalized Config whose composition performs no I/O: the
// server on a loopback ephemeral port, debug logging so requests leave
// records, and the database and NATS on closed loopback ports, so a Run
// fails its startup rather than reaching a real backing service. The empty
// prefix disables environment overrides.
func Config(t *testing.T) *config.Config {
	t.Helper()
	cfg := Minimal()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = new(int)
	cfg.Log.Level = logging.LevelDebug
	cfg.Database.Host = "127.0.0.1"
	cfg.Database.User = "app"
	port := ClosedPort(t)
	cfg.Database.Port = &port
	cfg.NATS.URL = fmt.Sprintf("nats://127.0.0.1:%d", ClosedPort(t))
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("finalize hermetic config: %v", err)
	}
	return cfg
}

// ClosedPort returns a loopback port nothing listens on: one the kernel
// assigned and the listener released.
func ClosedPort(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	if err := lis.Close(); err != nil {
		t.Fatalf("release the port: %v", err)
	}
	return port
}
