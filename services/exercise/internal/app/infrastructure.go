package app

import (
	"io"
	"log/slog"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/logging"
	"github.com/JaimeStill/spike-messaging/services/exercise/internal/config"
)

// Infrastructure holds the services an application is composed on, one
// concrete field per service: the logger in the template baseline; a
// database pool, storage, or auth client as a service grows. A field either
// exists or the build fails, so a wiring mistake surfaces at compile time;
// roles sharing a type (a write pool and a read pool) are distinct fields,
// distinguished by name. The struct stops at the composition root: the
// layer files read its fields, and a domain package receives its
// dependencies as constructor parameters, never the struct itself.
type Infrastructure struct {
	Logger *slog.Logger
}

// newInfrastructure constructs the infrastructure services in one place, in
// dependency order, each registering on lc where it is built — as a
// lifecycle.Service with the stage that places it in the process's startup
// order — so a service cannot exist without a startup, shutdown, or
// readiness declaration. Construction opens nothing: connectivity belongs
// to a service's Start hook, so a failed cold start leaks no connections.
// lc goes unused today, because the template's one service, Logger, has no
// lifecycle; it stays a parameter so the first service that needs one — a
// database pool, for instance — registers here without a signature change.
func newInfrastructure(
	w io.Writer,
	cfg *config.Config,
	lc *lifecycle.Coordinator,
) (*Infrastructure, error) {
	logger := logging.New(w, cfg.Log)

	return &Infrastructure{
		Logger: logger,
	}, nil
}
