// Package configtest builds hermetically valid service configuration for
// tests. It is the single place the suites learn what the root config
// requires: when a subsystem's block gains a required field, it is set here
// once and every consuming test adapts.
package configtest

import (
	"testing"

	"github.com/standards-lab/go-core/logging"
	"github.com/JaimeStill/spike-messaging/services/exercise/internal/config"
)

// Config returns a finalized Config whose composition performs no I/O: the
// server on a loopback ephemeral port, and debug logging so requests leave
// records. The empty prefix disables environment overrides.
func Config(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = new(int)
	cfg.Log.Level = logging.LevelDebug
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("finalize hermetic config: %v", err)
	}
	return cfg
}
