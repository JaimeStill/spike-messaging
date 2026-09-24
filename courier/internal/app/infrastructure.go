package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"
	pgdialect "github.com/standards-lab/sqlate/postgres"

	"github.com/JaimeStill/spike-messaging/courier/scenario"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/memory"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
	"github.com/JaimeStill/spike-messaging/messaging/outbox/postgres"
)

// Infrastructure builds the broker the flags name, and the outbox store on
// the Postgres server they name. It opens nothing that outlives a command:
// each scenario run builds its own broker and its own scratch database.
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

// dbWait bounds each call the outbox store makes to the server outside a
// scenario's own steps: the ping, and the drop in the cleanup.
const dbWait = 10 * time.Second

// OutboxNeeds returns what the outbox store requires: a Postgres server
// reachable at the DSN.
func (i *Infrastructure) OutboxNeeds() []scenario.Need {
	return []scenario.Need{{What: "Postgres at --dsn or $MESSAGING_DSN", Check: i.ping}}
}

func (i *Infrastructure) ping(ctx context.Context) error {
	dsn := i.cfg.dsn()
	if dsn == "" {
		return errors.New("no DSN: set --dsn or MESSAGING_DSN")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(ctx, dbWait)
	defer cancel()
	return db.PingContext(ctx)
}

// Outbox creates a uniquely named database on the server at the DSN,
// migrates the messaging set into it, verifies the outbox's statements
// against it, and returns the outbox over a pool connected to it. The
// store's Close closes the pool and drops the database.
func (i *Infrastructure) Outbox(ctx context.Context) (_ *scenario.OutboxStore, err error) {
	dsn := i.cfg.dsn()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		_ = admin.Close()
		return nil, err
	}
	name := "courier_outbox_" + hex.EncodeToString(b[:])
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		// A create cancelled by ctx can still finish on the server, so the
		// database is dropped if it exists, on a context of its own.
		dctx, cancel := context.WithTimeout(context.Background(), dbWait)
		_, derr := admin.ExecContext(dctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		cancel()
		_ = admin.Close()
		return nil, errors.Join(fmt.Errorf("create database %s: %w", name, err), derr)
	}

	var pool *sql.DB
	drop := func() error {
		if pool != nil {
			_ = pool.Close()
		}
		defer func() { _ = admin.Close() }()
		// WITH (FORCE) ends the database's other sessions, and the server can
		// still report one closing, so the drop retries.
		var err error
		for range 4 {
			ctx, cancel := context.WithTimeout(context.Background(), dbWait)
			_, err = admin.ExecContext(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
			cancel()
			if err == nil {
				return nil
			}
			time.Sleep(200 * time.Millisecond)
		}
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, drop())
		}
	}()

	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse the DSN: %w", err)
	}
	u.Path = "/" + name
	if pool, err = sql.Open("pgx", u.String()); err != nil {
		return nil, err
	}
	db := sqlate.Wrap(pool, pgdialect.Dialect{})
	set, err := postgres.Migrations()
	if err != nil {
		return nil, err
	}
	m, err := migrate.New(db, []migrate.Set{set}, migrate.Options{})
	if err != nil {
		return nil, err
	}
	if err := m.Up(ctx); err != nil {
		return nil, fmt.Errorf("migrate %s: %w", name, err)
	}
	if err := postgres.Verify(ctx, db); err != nil {
		return nil, fmt.Errorf("verify %s: %w", name, err)
	}
	ob, err := outbox.New(postgres.Engine())
	if err != nil {
		return nil, err
	}
	return &scenario.OutboxStore{
		Name:    name,
		DB:      db,
		Outbox:  ob,
		Pending: func(ctx context.Context) (int, error) { return postgres.Pending(ctx, db) },
		Close:   drop,
	}, nil
}
