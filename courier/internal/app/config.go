package app

import "github.com/spf13/pflag"

// Config holds the persistent flags, read by the layers once cobra has
// parsed them.
type Config struct {
	Broker string
}

// bind registers cfg's flags on fs.
func (cfg *Config) bind(fs *pflag.FlagSet) {
	fs.StringVar(&cfg.Broker, "broker", "memory", "the broker the scenarios run on: memory")
}
