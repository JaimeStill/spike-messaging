package nats

import (
	"errors"
	"fmt"
	"os"
	"time"

	libconfig "github.com/standards-lab/go-core/config"

	"github.com/JaimeStill/spike-messaging/messaging"
)

// DefaultURL is the NATS server Finalize fills in when a configuration names
// none.
const DefaultURL = "nats://127.0.0.1:4222"

// Config names the NATS server a broker connects to and the stream it
// publishes to and subscribes on.
type Config struct {
	// URL is the NATS server; Finalize defaults it to DefaultURL.
	URL string `json:"url"`
	// Name is the connection's name, which the server reports; optional.
	Name string `json:"name"`
	// Stream is the stream's name: a token, as a subscription's Name is.
	Stream string `json:"stream"`
	// Prefix is the subject prefix, one or more tokens; the stream captures
	// Prefix.> and an event is published to Prefix.<type>.
	Prefix string `json:"prefix"`
	// Duplicates is the stream's deduplication window; 0 is
	// [messaging.DefaultDuplicates].
	Duplicates libconfig.Duration `json:"duplicates"`
	// MaxAge bounds the stream's retention: the stream discards an event
	// older than MaxAge, whether or not every consumer has received it. A
	// consumer that falls further behind than MaxAge recovers when the
	// event's producer republishes its current state. 0 keeps every event.
	// JetStream requires a positive MaxAge to be at least the
	// deduplication window.
	MaxAge libconfig.Duration `json:"max_age"`
}

// Validate reports every way cfg is unusable.
func (cfg Config) Validate() error {
	var errs []error
	if cfg.URL == "" {
		errs = append(errs, errors.New("url is required"))
	}
	if err := messaging.CheckName(cfg.Stream); err != nil {
		errs = append(errs, fmt.Errorf("stream: %w", err))
	}
	if err := messaging.CheckType(cfg.Prefix); err != nil {
		errs = append(errs, fmt.Errorf("prefix: %w", err))
	}
	if cfg.Duplicates < 0 {
		errs = append(errs, errors.New("duplicates must not be negative"))
	}
	dupes := cfg.window()
	switch {
	case cfg.MaxAge < 0:
		errs = append(errs, errors.New("max age must not be negative"))
	case cfg.MaxAge > 0 && cfg.MaxAge.Duration() < dupes:
		errs = append(errs, fmt.Errorf("max age %v must be at least the deduplication window, %v", cfg.MaxAge, dupes))
	}
	if len(errs) > 0 {
		return fmt.Errorf("nats: config: %w", errors.Join(errs...))
	}
	return nil
}

// window is the stream's deduplication window: Duplicates, or
// [messaging.DefaultDuplicates] when it is 0.
func (cfg Config) window() time.Duration {
	if cfg.Duplicates == 0 {
		return messaging.DefaultDuplicates
	}
	return cfg.Duplicates.Duration()
}

// Merge overlays src's set fields onto the receiver.
func (cfg *Config) Merge(src *Config) {
	if src == nil {
		return
	}
	if src.URL != "" {
		cfg.URL = src.URL
	}
	if src.Name != "" {
		cfg.Name = src.Name
	}
	if src.Stream != "" {
		cfg.Stream = src.Stream
	}
	if src.Prefix != "" {
		cfg.Prefix = src.Prefix
	}
	if src.Duplicates != 0 {
		cfg.Duplicates = src.Duplicates
	}
	if src.MaxAge != 0 {
		cfg.MaxAge = src.MaxAge
	}
}

// Finalize defaults URL to [DefaultURL], reads the block's environment
// overrides when a prefix is given (<PREFIX>_NATS_URL, …_NAME, …_STREAM,
// …_PREFIX, …_DUPLICATES, …_MAX_AGE), and validates the result.
func (cfg *Config) Finalize(envPrefix string) error {
	if cfg.URL == "" {
		cfg.URL = DefaultURL
	}
	if envPrefix != "" {
		for key, dst := range map[string]*string{
			"nats_url":    &cfg.URL,
			"nats_name":   &cfg.Name,
			"nats_stream": &cfg.Stream,
			"nats_prefix": &cfg.Prefix,
		} {
			if v := os.Getenv(libconfig.EnvName(envPrefix, key)); v != "" {
				*dst = v
			}
		}
		for key, dst := range map[string]*libconfig.Duration{
			"nats_duplicates": &cfg.Duplicates,
			"nats_max_age":    &cfg.MaxAge,
		} {
			name := libconfig.EnvName(envPrefix, key)
			if err := dst.Set(os.Getenv(name)); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	return cfg.Validate()
}
