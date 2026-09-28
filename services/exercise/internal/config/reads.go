package config

import (
	"fmt"
	"os"
	"strconv"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-web-sdk"
)

// The service's paging policy defaults, applied by Finalize when the
// configuration leaves a field unset.
const (
	defaultReadsDefaultSize = 20
	defaultReadsMaxSize     = 100
)

// ReadsConfig is the service-owned read policy: the page size a request gets
// when it asks for none, and the largest size it may ask for. The SDK holds
// no policy numbers; this block is their single source, handed to each
// handler constructor as web.Limits at the route build point. Pointer fields
// distinguish unset from zero.
type ReadsConfig struct {
	DefaultSize *int `json:"default_size"`
	MaxSize     *int `json:"max_size"`
	// Env records the environment-variable names Finalize composed and read.
	Env ReadsEnv `json:"-"`
}

// ReadsEnv holds the environment-variable names of the reads block.
type ReadsEnv struct {
	DefaultSize string
	MaxSize     string
}

// Merge overlays src's set fields onto the receiver.
func (c *ReadsConfig) Merge(src *ReadsConfig) {
	if src == nil {
		return
	}
	if src.DefaultSize != nil {
		v := *src.DefaultSize
		c.DefaultSize = &v
	}
	if src.MaxSize != nil {
		v := *src.MaxSize
		c.MaxSize = &v
	}
}

// Finalize applies the defaults, reads the block's environment overrides
// when a prefix is given, and validates: DefaultSize at least 1 and MaxSize
// at least DefaultSize, the invariant web.ParseQuery panics on, caught here
// as configuration rather than at the first request. An empty prefix
// disables the overrides.
func (c *ReadsConfig) Finalize(envPrefix string) error {
	if c.DefaultSize == nil {
		c.DefaultSize = new(defaultReadsDefaultSize)
	}
	if c.MaxSize == nil {
		c.MaxSize = new(defaultReadsMaxSize)
	}
	if envPrefix != "" {
		c.Env.DefaultSize = libconfig.EnvName(envPrefix, "reads_default_size")
		c.Env.MaxSize = libconfig.EnvName(envPrefix, "reads_max_size")
		if err := setIntFromEnv(c.DefaultSize, c.Env.DefaultSize); err != nil {
			return err
		}
		if err := setIntFromEnv(c.MaxSize, c.Env.MaxSize); err != nil {
			return err
		}
	}
	if *c.DefaultSize < 1 {
		return fmt.Errorf("default_size must be at least 1, got %d", *c.DefaultSize)
	}
	if *c.MaxSize < *c.DefaultSize {
		return fmt.Errorf("max_size must be at least default_size (%d), got %d", *c.DefaultSize, *c.MaxSize)
	}
	return nil
}

// Limits hands the finalized policy to a handler constructor.
func (c ReadsConfig) Limits() web.Limits {
	return web.Limits{DefaultSize: *c.DefaultSize, MaxSize: *c.MaxSize}
}

// setIntFromEnv overwrites dst with the integer named by name when it is set.
func setIntFromEnv(dst *int, name string) error {
	v := os.Getenv(name)
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	*dst = n
	return nil
}
