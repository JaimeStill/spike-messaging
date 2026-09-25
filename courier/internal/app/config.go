package app

import (
	"os"

	"github.com/spf13/pflag"
)

// Config holds the persistent flags, read by the layers once cobra has
// parsed them: the broker the scenarios run on, the NATS server the nats
// broker connects to, and the Postgres server the outbox scenario uses.
type Config struct {
	Broker  string
	NATSURL string
	DSN     string
}

// bind registers cfg's flags on fs.
func (cfg *Config) bind(fs *pflag.FlagSet) {
	fs.StringVar(&cfg.Broker, "broker", "memory", "the broker the scenarios run on: memory or nats")
	fs.StringVar(&cfg.NATSURL, "nats-url", "", "the NATS server the nats broker creates its scratch stream on (default $MESSAGING_NATS_URL)")
	fs.StringVar(&cfg.DSN, "dsn", "", "the Postgres server the outbox scenario creates its scratch database on (default $MESSAGING_DSN)")
}

// dsn is the --dsn flag, or $MESSAGING_DSN when the flag is unset. The
// environment is read here rather than as the flag's default, so the help
// never prints the DSN's password.
func (cfg *Config) dsn() string {
	if cfg.DSN != "" {
		return cfg.DSN
	}
	return os.Getenv("MESSAGING_DSN")
}

// natsURL is the --nats-url flag, or $MESSAGING_NATS_URL when the flag is
// unset, read here for the same reason as dsn: a URL can carry credentials.
func (cfg *Config) natsURL() string {
	if cfg.NATSURL != "" {
		return cfg.NATSURL
	}
	return os.Getenv("MESSAGING_NATS_URL")
}
