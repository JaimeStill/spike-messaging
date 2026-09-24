package app

import (
	"os"

	"github.com/spf13/pflag"
)

// Config holds the persistent flags, read by the layers once cobra has
// parsed them.
type Config struct {
	Broker string
	DSN    string
}

// bind registers cfg's flags on fs.
func (cfg *Config) bind(fs *pflag.FlagSet) {
	fs.StringVar(&cfg.Broker, "broker", "memory", "the broker the scenarios run on: memory")
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
