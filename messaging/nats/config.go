package nats

import (
	"fmt"
	"os"

	libconfig "github.com/standards-lab/go-core/config"
)

// Merge overlays src's set fields onto the receiver.
func (cfg *Config) Merge(src *Config) {
	if src == nil {
		return
	}
	for dst, v := range map[*string]string{
		&cfg.URL:    src.URL,
		&cfg.Name:   src.Name,
		&cfg.Stream: src.Stream,
		&cfg.Prefix: src.Prefix,
	} {
		if v != "" {
			*dst = v
		}
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
