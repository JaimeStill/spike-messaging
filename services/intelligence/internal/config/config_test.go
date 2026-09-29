package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/nats"
	"github.com/JaimeStill/spike-messaging/services/intelligence/internal/config"
	"github.com/JaimeStill/spike-messaging/services/intelligence/internal/config/configtest"
	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/logging"
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
	cfg := configtest.Minimal()
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
	cfg := configtest.Minimal()
	if err := cfg.Finalize("intelligence"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if got := cfg.Log.Env.Level; got != "INTELLIGENCE_LOG_LEVEL" {
		t.Errorf("Log.Env.Level = %s, want INTELLIGENCE_LOG_LEVEL", got)
	}
	if got := cfg.Server.Env.Port; got != "INTELLIGENCE_SERVER_PORT" {
		t.Errorf("Server.Env.Port = %s, want INTELLIGENCE_SERVER_PORT", got)
	}
}

func TestConfig_FinalizeEnvOverrides(t *testing.T) {
	t.Setenv("INTELLIGENCE_SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("INTELLIGENCE_LOG_LEVEL", "debug")
	t.Setenv("INTELLIGENCE_SERVER_PORT", "9090")

	cfg := configtest.Minimal()
	if err := cfg.Finalize("intelligence"); err != nil {
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
	t.Setenv("INTELLIGENCE_SHUTDOWN_TIMEOUT", "-5s")

	cfg := configtest.Minimal()
	err := cfg.Finalize("intelligence")
	if err == nil {
		t.Fatal("Finalize accepted a negative shutdown_timeout")
	}
	if !strings.Contains(err.Error(), "shutdown_timeout") {
		t.Errorf("error = %v, want it to name shutdown_timeout", err)
	}
}

func TestConfig_FinalizeWrapsChildErrors(t *testing.T) {
	t.Setenv("INTELLIGENCE_LOG_LEVEL", "verbose")

	cfg := configtest.Minimal()
	err := cfg.Finalize("intelligence")
	if err == nil {
		t.Fatal("Finalize accepted an invalid log level")
	}
	if !strings.Contains(err.Error(), "log:") {
		t.Errorf("error = %v, want the log block wrap", err)
	}
}

func TestMessaging_FinalizeDefaults(t *testing.T) {
	cfg := configtest.Minimal()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	// Pins the documented messaging and nats defaults.
	if cfg.NATS.URL != nats.DefaultURL {
		t.Errorf("URL = %s, want the default NATS URL", cfg.NATS.URL)
	}
	if cfg.Messaging.RelayPoll.Duration() != messaging.DefaultRelayPoll {
		t.Errorf("RelayPoll = %s, want %s", cfg.Messaging.RelayPoll, messaging.DefaultRelayPoll)
	}
}

func TestMessaging_FinalizeEnvOverrides(t *testing.T) {
	t.Setenv("INTELLIGENCE_NATS_URL", "nats://nats.internal:4222")
	t.Setenv("INTELLIGENCE_NATS_STREAM", "scratch")
	t.Setenv("INTELLIGENCE_NATS_PREFIX", "scratch.a1")
	t.Setenv("INTELLIGENCE_NATS_MAX_AGE", "1h")
	t.Setenv("INTELLIGENCE_MESSAGING_SOURCE", "/intelligence-b")
	t.Setenv("INTELLIGENCE_MESSAGING_RELAY_POLL", "1s")

	cfg := configtest.Minimal()
	if err := cfg.Finalize("intelligence"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	wantMessaging := messaging.Config{Source: "/intelligence-b", RelayPoll: libconfig.Duration(time.Second)}
	if cfg.Messaging != wantMessaging {
		t.Errorf("Messaging = %+v, want %+v", cfg.Messaging, wantMessaging)
	}
	wantNATS := nats.Config{
		URL:    "nats://nats.internal:4222",
		Stream: "scratch",
		Prefix: "scratch.a1",
		MaxAge: libconfig.Duration(time.Hour),
	}
	if cfg.NATS != wantNATS {
		t.Errorf("NATS = %+v, want %+v", cfg.NATS, wantNATS)
	}
}

func TestMessaging_FinalizeRejectsAnIncompleteBlock(t *testing.T) {
	cfg := configtest.Minimal()
	cfg.Messaging.Source = ""
	cfg.Messaging.RelayPoll = libconfig.Duration(-time.Second)
	err := cfg.Finalize("")
	if err == nil {
		t.Fatal("Finalize accepted an incomplete messaging block")
	}
	for _, want := range []string{"messaging:", "source is required", "relay_poll must be positive"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to name %q", err, want)
		}
	}

	cfg = configtest.Minimal()
	cfg.NATS.Stream = ""
	err = cfg.Finalize("")
	if err == nil || !strings.Contains(err.Error(), "nats:") || !strings.Contains(err.Error(), "stream") {
		t.Errorf("Finalize = %v, want the nats block's missing stream", err)
	}
}

func TestIntelligence_FinalizeDefaults(t *testing.T) {
	cfg := configtest.Minimal()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	// Pins the documented contact rounds default.
	if cfg.Intelligence.ContactRounds != 3 {
		t.Errorf("ContactRounds = %d, want 3", cfg.Intelligence.ContactRounds)
	}
}

func TestIntelligence_MergeOverlaysSetFields(t *testing.T) {
	base := &config.Config{Intelligence: config.Intelligence{ContactRounds: 5}}

	base.Merge(&config.Config{})
	if base.Intelligence.ContactRounds != 5 {
		t.Errorf("ContactRounds = %d, want the base's 5 kept", base.Intelligence.ContactRounds)
	}

	base.Merge(&config.Config{Intelligence: config.Intelligence{ContactRounds: 2}})
	if base.Intelligence.ContactRounds != 2 {
		t.Errorf("ContactRounds = %d, want the overlay's 2", base.Intelligence.ContactRounds)
	}
}

func TestIntelligence_FinalizeEnvOverride(t *testing.T) {
	t.Setenv("INTELLIGENCE_CONTACT_ROUNDS", "7")

	cfg := configtest.Minimal()
	if err := cfg.Finalize("intelligence"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if cfg.Intelligence.ContactRounds != 7 {
		t.Errorf("ContactRounds = %d, want 7", cfg.Intelligence.ContactRounds)
	}

	t.Setenv("INTELLIGENCE_CONTACT_ROUNDS", "many")
	err := configtest.Minimal().Finalize("intelligence")
	if err == nil || !strings.Contains(err.Error(), "INTELLIGENCE_CONTACT_ROUNDS") {
		t.Errorf("Finalize = %v, want the malformed override named", err)
	}
}

func TestIntelligence_FinalizeRejectsFewerThanOneRound(t *testing.T) {
	cfg := configtest.Minimal()
	cfg.Intelligence.ContactRounds = -1
	err := cfg.Finalize("")
	if err == nil {
		t.Fatal("Finalize accepted contact_rounds below 1")
	}
	for _, want := range []string{"intelligence:", "contact_rounds must be at least 1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to name %q", err, want)
		}
	}
}
