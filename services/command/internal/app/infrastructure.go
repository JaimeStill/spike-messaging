package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/admin"
	dbpostgres "github.com/standards-lab/go-database/postgres"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/query"

	corelifecycle "github.com/JaimeStill/spike-messaging/core/lifecycle"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/nats"
	"github.com/JaimeStill/spike-messaging/messaging/postgres"
	"github.com/JaimeStill/spike-messaging/services/command/data"
	"github.com/JaimeStill/spike-messaging/services/command/internal/config"
)

// Infrastructure holds the services the application is composed on, one
// concrete field per service. DB is the database's lifecycle object, which
// the admin service administers; the domains never see it. SQL is the
// database as the domains see it. Sets are the migration sets the admin
// service's migrator runs, the messaging set beneath the service's own.
// Broker is the NATS broker, a lifecycle component. Messaging is the
// service's messaging over it: the outbox, the inbox, and the recorder, the
// one value a domain takes to emit the events it raises. The struct stays in
// the composition root: the layer files read its fields, and a package
// receives its dependencies as constructor parameters, never the struct
// itself.
type Infrastructure struct {
	Logger    *slog.Logger
	DB        *database.DB
	SQL       *data.Database
	Sets      []migrate.Set
	Broker    *nats.Broker
	Messaging *messaging.Runtime
}

// verifyStage is the stage that prepares the messaging statements, and each
// domain's own, against the migrated schema, after the schema service at
// admin.Stage has corrected it.
const verifyStage = admin.Stage + 1

// newInfrastructure constructs the infrastructure services in one place, in
// dependency order. Each registers on lc where it is built, at the stage
// that places it in the process's startup order: a component through
// corelifecycle.Register, and the start-only verification as a
// lifecycle.Service. A service therefore cannot exist without a startup,
// shutdown, or readiness declaration. The database and the broker register
// at stage 0, so they start first and drain last, after every reactor.
// Construction opens nothing: connectivity belongs to a service's Start
// hook, so a failed cold start leaks no connections. This file is the one
// place a provider is named: the database's, the broker's, and the
// messaging engine with its migration set.
func newInfrastructure(
	w io.Writer,
	cfg *config.Config,
	lc *lifecycle.Coordinator,
) (*Infrastructure, error) {
	logger := logging.New(w, cfg.Log)

	db, err := dbpostgres.New(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	corelifecycle.Register(lc, "database", 0, db)

	b, err := nats.New(cfg.NATS)
	if err != nil {
		return nil, fmt.Errorf("broker: %w", err)
	}
	corelifecycle.Register(lc, "broker", 0, b)

	catalog := query.MustCatalog(query.Patterns())
	session := sqlate.Wrap(db.Conn(), pgdialect.Dialect{})

	msg, err := messaging.New(cfg.Messaging, b, postgres.Outbox(), postgres.Inbox(), cfg.ShutdownTimeout.Duration(), logger)
	if err != nil {
		return nil, err
	}
	lc.Add(lifecycle.Service{
		Name:  "messaging",
		Stage: verifyStage,
		Start: func(ctx context.Context) error { return postgres.Verify(ctx, session) },
	})
	messagingSet, err := postgres.Migrations()
	if err != nil {
		return nil, fmt.Errorf("messaging migrations: %w", err)
	}

	return &Infrastructure{
		Logger:    logger,
		DB:        db,
		SQL:       data.New(session, catalog),
		Sets:      data.Migrations(messagingSet),
		Broker:    b,
		Messaging: msg,
	}, nil
}
