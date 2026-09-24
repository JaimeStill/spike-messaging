package app

import (
	"fmt"

	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/memory"
	"github.com/JaimeStill/spike-messaging/courier/scenario"
)

// Infrastructure builds the broker the flags name. It opens nothing that
// outlives a command: each scenario run builds its own broker.
type Infrastructure struct {
	cfg *Config
}

func newInfrastructure(cfg *Config) *Infrastructure {
	return &Infrastructure{cfg: cfg}
}

// Validate fails when the flags name a broker courier has no provider for.
func (i *Infrastructure) Validate() error {
	_, err := i.Broker()
	return err
}

// Broker returns a fresh broker of the kind the flags name.
func (i *Infrastructure) Broker() (messaging.Broker, error) {
	switch i.cfg.Broker {
	case "memory":
		return memory.New(), nil
	default:
		return nil, fmt.Errorf("unknown broker %q (known: memory)", i.cfg.Broker)
	}
}

// Needs returns what the broker requires to run, which each scenario checks
// first. The memory broker needs nothing.
func (i *Infrastructure) Needs() []scenario.Need {
	return nil
}
