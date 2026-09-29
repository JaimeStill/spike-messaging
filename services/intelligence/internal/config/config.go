package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-web-sdk"

	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/nats"
)

// envPrefix namespaces the service's environment variables ("intelligence" →
// INTELLIGENCE_LOG_LEVEL); a seeded service renames its whole namespace here.
const envPrefix = "intelligence"

const defaultShutdownTimeout = 10 * time.Second

// DefaultContactRounds is how many rounds a contact stays in an assessment
// unseen before it drops, when a configuration sets none.
const DefaultContactRounds = 3

// Intelligence is the service's own block: the setting of how it fuses a
// faction's observations. ContactRounds is the number of rounds a contact
// stays in an assessment unseen before it drops.
type Intelligence struct {
	ContactRounds int `json:"contact_rounds"`
}

// Merge overlays src's set fields onto the receiver.
func (i *Intelligence) Merge(src *Intelligence) {
	if src == nil {
		return
	}
	if src.ContactRounds != 0 {
		i.ContactRounds = src.ContactRounds
	}
}

// Finalize defaults ContactRounds to [DefaultContactRounds], reads the
// block's environment override when a prefix is given
// (<PREFIX>_CONTACT_ROUNDS), and validates that the rounds are at least one.
func (i *Intelligence) Finalize(envPrefix string) error {
	if i.ContactRounds == 0 {
		i.ContactRounds = DefaultContactRounds
	}
	if envPrefix != "" {
		name := libconfig.EnvName(envPrefix, "contact_rounds")
		if v := os.Getenv(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			i.ContactRounds = n
		}
	}
	if i.ContactRounds < 1 {
		return fmt.Errorf("contact_rounds must be at least 1, got %d", i.ContactRounds)
	}
	return nil
}

// Config is the service's root configuration: the library capability blocks,
// the database block, the messaging block, and the nats block of its
// broker, and the intelligence block, plus the service-owned shutdown
// timeout.
type Config struct {
	Log             logging.Config     `json:"log"`
	Server          web.Config         `json:"server"`
	Database        database.Config    `json:"database"`
	Messaging       messaging.Config   `json:"messaging"`
	NATS            nats.Config        `json:"nats"`
	Intelligence    Intelligence       `json:"intelligence"`
	ShutdownTimeout libconfig.Duration `json:"shutdown_timeout"`
}

// Merge overlays src's set fields onto the receiver, delegating each block
// to its own Merge.
func (c *Config) Merge(src *Config) {
	if src == nil {
		return
	}
	if src.ShutdownTimeout != 0 {
		c.ShutdownTimeout = src.ShutdownTimeout
	}
	c.Log.Merge(&src.Log)
	c.Server.Merge(&src.Server)
	c.Database.Merge(&src.Database)
	c.Messaging.Merge(&src.Messaging)
	c.NATS.Merge(&src.NATS)
	c.Intelligence.Merge(&src.Intelligence)
}

// Finalize applies the root default, reads the root's own environment
// override when a prefix is given, validates, and finalizes each block under
// the same prefix. It satisfies the config package's Load contract; an empty
// prefix disables every environment override — the hermetic form tests use.
func (c *Config) Finalize(envPrefix string) error {
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = libconfig.Duration(defaultShutdownTimeout)
	}

	if envPrefix != "" {
		name := libconfig.EnvName(envPrefix, "shutdown_timeout")
		if v := os.Getenv(name); v != "" {
			if err := c.ShutdownTimeout.Set(v); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}

	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown_timeout must be positive, got %s", c.ShutdownTimeout)
	}

	if err := c.Log.Finalize(envPrefix); err != nil {
		return fmt.Errorf("log: %w", err)
	}
	if err := c.Server.Finalize(envPrefix); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	if err := c.Database.Finalize(envPrefix); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	if err := c.Messaging.Finalize(envPrefix); err != nil {
		return fmt.Errorf("messaging: %w", err)
	}
	if err := c.NATS.Finalize(envPrefix); err != nil {
		return fmt.Errorf("nats: %w", err)
	}
	if err := c.Intelligence.Finalize(envPrefix); err != nil {
		return fmt.Errorf("intelligence: %w", err)
	}
	return nil
}

// Load reads the layered configuration files and finalizes the result under
// the service's env prefix.
func Load() (*Config, error) {
	return libconfig.Load[Config](libconfig.Options{
		EnvPrefix: envPrefix,
	})
}
