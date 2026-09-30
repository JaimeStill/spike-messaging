package nats_test

import (
	"testing"
	"time"

	libconfig "github.com/standards-lab/go-core/config"

	"github.com/JaimeStill/spike-messaging/messaging/nats"
)

func TestConfigValidate(t *testing.T) {
	for name, cfg := range map[string]nats.Config{
		"no url":          {Stream: "s", Prefix: "p"},
		"no stream":       {URL: "u", Prefix: "p"},
		"dotted stream":   {URL: "u", Stream: "a.b", Prefix: "p"},
		"slashed stream":  {URL: "u", Stream: "a/b", Prefix: "p"},
		"no prefix":       {URL: "u", Stream: "s"},
		"wildcard":        {URL: "u", Stream: "s", Prefix: "p.*"},
		"negative dupes":  {URL: "u", Stream: "s", Prefix: "p", Duplicates: -1},
		"negative age":    {URL: "u", Stream: "s", Prefix: "p", MaxAge: -1},
		"age below dupes": {URL: "u", Stream: "s", Prefix: "p", MaxAge: libconfig.Duration(time.Minute)},
	} {
		if cfg.Validate() == nil {
			t.Errorf("%s: Validate accepted %+v", name, cfg)
		}
		if _, err := nats.New(cfg); err == nil {
			t.Errorf("%s: New accepted %+v", name, cfg)
		}
	}
}

// Finalize fills the default URL, applies the block's environment
// overrides over the merged file values, and validates.
func TestConfigFinalize(t *testing.T) {
	cfg := nats.Config{Stream: "file", Prefix: "file"}
	cfg.Merge(&nats.Config{Stream: "overlay", MaxAge: libconfig.Duration(time.Hour)})
	t.Setenv("SVC_NATS_PREFIX", "env")
	t.Setenv("SVC_NATS_MAX_AGE", "2h")
	if err := cfg.Finalize("svc"); err != nil {
		t.Fatal(err)
	}
	want := nats.Config{URL: nats.DefaultURL, Stream: "overlay", Prefix: "env", MaxAge: libconfig.Duration(2 * time.Hour)}
	if cfg != want {
		t.Errorf("Finalize = %+v, want %+v", cfg, want)
	}

	t.Setenv("SVC_NATS_STREAM", "not.a.token")
	if err := cfg.Finalize("svc"); err == nil {
		t.Error("Finalize accepted a dotted stream from the environment")
	}
}
