package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
)

// The messaging block's defaults, applied by Finalize when the
// configuration leaves a field unset.
const (
	defaultMessagingURL       = "nats://127.0.0.1:4222"
	defaultMessagingMaxAge    = 24 * time.Hour
	defaultMessagingRelayPoll = 250 * time.Millisecond
)

// MessagingConfig is the event layer's block. URL is the NATS server the
// broker connects to. Stream and Prefix name the JetStream stream every
// exercise service shares, so each service provisions the same stream and
// they converge on it; MaxAge bounds its retention, longer than an
// exercise. Source is the service's CloudEvents source, one value for every
// replica. RelayPoll is how long the relay waits between passes once the
// outbox is empty.
type MessagingConfig struct {
	URL       string             `json:"url"`
	Stream    string             `json:"stream"`
	Prefix    string             `json:"prefix"`
	MaxAge    libconfig.Duration `json:"max_age"`
	Source    string             `json:"source"`
	RelayPoll libconfig.Duration `json:"relay_poll"`
}

// Merge overlays src's set fields onto the receiver.
func (c *MessagingConfig) Merge(src *MessagingConfig) {
	if src == nil {
		return
	}
	if src.URL != "" {
		c.URL = src.URL
	}
	if src.Stream != "" {
		c.Stream = src.Stream
	}
	if src.Prefix != "" {
		c.Prefix = src.Prefix
	}
	if src.MaxAge != 0 {
		c.MaxAge = src.MaxAge
	}
	if src.Source != "" {
		c.Source = src.Source
	}
	if src.RelayPoll != 0 {
		c.RelayPoll = src.RelayPoll
	}
}

// Finalize applies the defaults, reads the block's environment overrides
// when a prefix is given (EXERCISE_MESSAGING_URL, …_STREAM, …_PREFIX,
// …_MAX_AGE, …_SOURCE, …_RELAY_POLL), and validates: the stream, prefix,
// and source set, and both durations positive. The broker checks the
// stream and prefix against its naming rules when the root builds it.
func (c *MessagingConfig) Finalize(envPrefix string) error {
	if c.URL == "" {
		c.URL = defaultMessagingURL
	}
	if c.MaxAge == 0 {
		c.MaxAge = libconfig.Duration(defaultMessagingMaxAge)
	}
	if c.RelayPoll == 0 {
		c.RelayPoll = libconfig.Duration(defaultMessagingRelayPoll)
	}
	if envPrefix != "" {
		for key, dst := range map[string]*string{
			"messaging_url":    &c.URL,
			"messaging_stream": &c.Stream,
			"messaging_prefix": &c.Prefix,
			"messaging_source": &c.Source,
		} {
			if v := os.Getenv(libconfig.EnvName(envPrefix, key)); v != "" {
				*dst = v
			}
		}
		for key, dst := range map[string]*libconfig.Duration{
			"messaging_max_age":    &c.MaxAge,
			"messaging_relay_poll": &c.RelayPoll,
		} {
			name := libconfig.EnvName(envPrefix, key)
			if err := dst.Set(os.Getenv(name)); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	var errs []error
	for _, f := range []struct{ key, v string }{{"stream", c.Stream}, {"prefix", c.Prefix}, {"source", c.Source}} {
		if f.v == "" {
			errs = append(errs, fmt.Errorf("%s is required", f.key))
		}
	}
	if c.MaxAge <= 0 {
		errs = append(errs, fmt.Errorf("max_age must be positive, got %s", c.MaxAge))
	}
	if c.RelayPoll <= 0 {
		errs = append(errs, fmt.Errorf("relay_poll must be positive, got %s", c.RelayPoll))
	}
	return errors.Join(errs...)
}
