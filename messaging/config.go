package messaging

import (
	"errors"
	"fmt"
	"os"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
)

// DefaultRelayPoll is how long the relay waits between passes once the
// outbox is empty, when a configuration sets none.
const DefaultRelayPoll = 250 * time.Millisecond

// Config is a service's messaging block, the part of its messaging that
// names no broker: the provider's own block, such as nats.Config, sits
// beside it. Source is the service's CloudEvents source, one value for
// every replica. RelayPoll is how long the relay waits between passes once
// the outbox is empty.
type Config struct {
	Source    string             `json:"source"`
	RelayPoll libconfig.Duration `json:"relay_poll"`
}

// Merge overlays src's set fields onto the receiver.
func (c *Config) Merge(src *Config) {
	if src == nil {
		return
	}
	if src.Source != "" {
		c.Source = src.Source
	}
	if src.RelayPoll != 0 {
		c.RelayPoll = src.RelayPoll
	}
}

// Finalize defaults RelayPoll to [DefaultRelayPoll], reads the block's
// environment overrides when a prefix is given (<PREFIX>_MESSAGING_SOURCE
// and …_RELAY_POLL), and validates that the source is set and the poll is
// positive.
func (c *Config) Finalize(envPrefix string) error {
	if c.RelayPoll == 0 {
		c.RelayPoll = libconfig.Duration(DefaultRelayPoll)
	}
	if envPrefix != "" {
		if v := os.Getenv(libconfig.EnvName(envPrefix, "messaging_source")); v != "" {
			c.Source = v
		}
		name := libconfig.EnvName(envPrefix, "messaging_relay_poll")
		if err := c.RelayPoll.Set(os.Getenv(name)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	var errs []error
	if c.Source == "" {
		errs = append(errs, errors.New("source is required"))
	}
	if c.RelayPoll <= 0 {
		errs = append(errs, fmt.Errorf("relay_poll must be positive, got %s", c.RelayPoll))
	}
	return errors.Join(errs...)
}
