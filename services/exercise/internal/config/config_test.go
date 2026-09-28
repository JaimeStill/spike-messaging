package config_test

import (
	"strings"
	"testing"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-web-sdk"
	"github.com/JaimeStill/spike-messaging/services/exercise/internal/config"
)

func TestConfig_MergeOverlaysSetFields(t *testing.T) {
	base := &config.Config{ShutdownTimeout: libconfig.Duration(10 * time.Second)}
	base.Log.Level = logging.LevelInfo
	base.Server.Host = "0.0.0.0"

	overlay := &config.Config{}
	overlay.Log.Level = logging.LevelDebug
	overlay.Server.Host = "127.0.0.1"

	base.Merge(overlay)

	if base.Log.Level != logging.LevelDebug {
		t.Errorf("Log.Level = %s, want debug", base.Log.Level)
	}
	if base.Server.Host != "127.0.0.1" {
		t.Errorf("Server.Host = %s, want 127.0.0.1", base.Server.Host)
	}
	// A field the overlay leaves unset keeps the base value.
	if got := base.ShutdownTimeout.Duration(); got != 10*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 10s", got)
	}
}

func TestConfig_FinalizeDefaults(t *testing.T) {
	cfg := &config.Config{}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	// Pins the documented default shutdown timeout.
	if got := cfg.ShutdownTimeout.Duration(); got != 10*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 10s", got)
	}
	if cfg.Log.Level != logging.LevelInfo {
		t.Errorf("Log.Level = %s, want info", cfg.Log.Level)
	}
	if got := cfg.Server.Addr(); got != "0.0.0.0:8080" {
		t.Errorf("Server.Addr() = %s, want 0.0.0.0:8080", got)
	}
}

// Every environment-variable name derives from the prefix Finalize receives —
// in production, the one envPrefix const Load passes, the single place a
// seeded service renames.
func TestConfig_FinalizeSeedsEnvNamesFromPrefix(t *testing.T) {
	cfg := &config.Config{}
	if err := cfg.Finalize("exercise"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if got := cfg.Log.Env.Level; got != "EXERCISE_LOG_LEVEL" {
		t.Errorf("Log.Env.Level = %s, want EXERCISE_LOG_LEVEL", got)
	}
	if got := cfg.Server.Env.Port; got != "EXERCISE_SERVER_PORT" {
		t.Errorf("Server.Env.Port = %s, want EXERCISE_SERVER_PORT", got)
	}
}

func TestConfig_FinalizeEnvOverrides(t *testing.T) {
	t.Setenv("EXERCISE_SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("EXERCISE_LOG_LEVEL", "debug")
	t.Setenv("EXERCISE_SERVER_PORT", "9090")

	cfg := &config.Config{}
	if err := cfg.Finalize("exercise"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if got := cfg.ShutdownTimeout.Duration(); got != 30*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 30s", got)
	}
	if cfg.Log.Level != logging.LevelDebug {
		t.Errorf("Log.Level = %s, want debug", cfg.Log.Level)
	}
	if cfg.Server.Port == nil || *cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %v, want 9090", cfg.Server.Port)
	}
}

func TestConfig_FinalizeRejectsNonPositiveShutdownTimeout(t *testing.T) {
	t.Setenv("EXERCISE_SHUTDOWN_TIMEOUT", "-5s")

	cfg := &config.Config{}
	err := cfg.Finalize("exercise")
	if err == nil {
		t.Fatal("Finalize accepted a negative shutdown_timeout")
	}
	if !strings.Contains(err.Error(), "shutdown_timeout") {
		t.Errorf("error = %v, want it to name shutdown_timeout", err)
	}
}

func TestConfig_FinalizeWrapsChildErrors(t *testing.T) {
	t.Setenv("EXERCISE_LOG_LEVEL", "verbose")

	cfg := &config.Config{}
	err := cfg.Finalize("exercise")
	if err == nil {
		t.Fatal("Finalize accepted an invalid log level")
	}
	if !strings.Contains(err.Error(), "log:") {
		t.Errorf("error = %v, want the log block wrap", err)
	}
}

func TestReads_FinalizeDefaults(t *testing.T) {
	cfg := &config.Config{}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	// Pins the documented paging defaults.
	if got := cfg.Reads.Limits(); got != (web.Limits{DefaultSize: 20, MaxSize: 100}) {
		t.Errorf("Reads.Limits() = %+v, want {20 100}", got)
	}
	if cfg.Reads.Env != (config.ReadsEnv{}) {
		t.Errorf("Reads.Env = %+v, want empty under the empty prefix", cfg.Reads.Env)
	}
}

func TestReads_MergeOverlaysSetFields(t *testing.T) {
	base := &config.Config{}
	base.Reads.DefaultSize = new(10)
	base.Reads.MaxSize = new(50)

	overlay := &config.Config{}
	overlay.Reads.MaxSize = new(200)

	base.Merge(overlay)

	if got := *base.Reads.MaxSize; got != 200 {
		t.Errorf("Reads.MaxSize = %d, want 200", got)
	}
	// A field the overlay leaves unset keeps the base value.
	if got := *base.Reads.DefaultSize; got != 10 {
		t.Errorf("Reads.DefaultSize = %d, want 10", got)
	}
}

func TestReads_FinalizeEnvOverrides(t *testing.T) {
	t.Setenv("EXERCISE_READS_DEFAULT_SIZE", "25")
	t.Setenv("EXERCISE_READS_MAX_SIZE", "250")

	cfg := &config.Config{}
	if err := cfg.Finalize("exercise"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if got := cfg.Reads.Limits(); got != (web.Limits{DefaultSize: 25, MaxSize: 250}) {
		t.Errorf("Reads.Limits() = %+v, want {25 250}", got)
	}
	want := config.ReadsEnv{DefaultSize: "EXERCISE_READS_DEFAULT_SIZE", MaxSize: "EXERCISE_READS_MAX_SIZE"}
	if cfg.Reads.Env != want {
		t.Errorf("Reads.Env = %+v, want %+v", cfg.Reads.Env, want)
	}
}

func TestReads_FinalizeRejectsInvalidPolicy(t *testing.T) {
	cases := map[string]struct {
		defaultSize, maxSize string
		want                 string
	}{
		"default below one":    {"0", "100", "default_size"},
		"max below default":    {"20", "10", "max_size"},
		"default not a number": {"twenty", "100", "EXERCISE_READS_DEFAULT_SIZE"},
		"max not a number":     {"20", "many", "EXERCISE_READS_MAX_SIZE"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("EXERCISE_READS_DEFAULT_SIZE", tc.defaultSize)
			t.Setenv("EXERCISE_READS_MAX_SIZE", tc.maxSize)

			cfg := &config.Config{}
			err := cfg.Finalize("exercise")
			if err == nil {
				t.Fatal("Finalize accepted an invalid reads policy")
			}
			if !strings.Contains(err.Error(), "reads:") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want the reads block wrap naming %s", err, tc.want)
			}
		})
	}
}
