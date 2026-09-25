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
	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"
	pgdialect "github.com/standards-lab/sqlate/postgres"

	"github.com/JaimeStill/spike-messaging/courier/scenario"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/memory"
	"github.com/JaimeStill/spike-messaging/messaging/nats"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
	"github.com/JaimeStill/spike-messaging/messaging/outbox/postgres"
)

// Infrastructure builds the broker the flags name, and the outbox store on
// the Postgres server they name. It opens nothing that outlives a command:
// each scenario run builds its own broker, on a scratch stream for nats, and
// its own scratch database, and its cleanup removes them.
type Infrastructure struct {
	cfg *Config
}

func newInfrastructure(cfg *Config) *Infrastructure {
	return &Infrastructure{cfg: cfg}
}

// Validate fails when the flags name a broker courier has no provider for.
// It performs no I/O: what a broker needs from the network is its Needs.
func (i *Infrastructure) Validate() error {
	switch i.cfg.Broker {
	case "memory", "nats":
		return nil
	default:
		return fmt.Errorf("unknown broker %q (known: memory, nats)", i.cfg.Broker)
	}
}

// Broker returns a fresh broker of the kind the flags name, and the release
// that frees it. The memory broker needs no release. The nats broker runs on
// a stream of its own, courier_<hex> capturing courier.<hex>.>, which the
// release deletes before it drains the connection.
func (i *Infrastructure) Broker() (messaging.Broker, func() error, error) {
	if err := i.Validate(); err != nil {
		return nil, nil, err
	}
	if i.cfg.Broker == "nats" {
		return i.natsBroker()
	}
	return memory.New(), nil, nil
}

// natsWait bounds each call courier makes to the NATS server outside a
// scenario's own steps: the connect, the stream's creation and deletion, and
// the drain.
const natsWait = 10 * time.Second

func (i *Infrastructure) natsBroker() (messaging.Broker, func() error, error) {
	nc, err := i.natsConnect()
	if err != nil {
		return nil, nil, err
	}
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		nc.Close()
		return nil, nil, err
	}
	suffix := hex.EncodeToString(b[:])
	ctx, cancel := context.WithTimeout(context.Background(), natsWait)
	defer cancel()
	broker, err := nats.New(ctx, nc, nats.Config{Stream: "courier_" + suffix, Prefix: "courier." + suffix})
	if err != nil {
		// A create cut short by the timeout can still finish on the server,
		// so the stream is deleted if it exists before the connection closes.
		derr := deleteStream(nc, "courier_"+suffix)
		nc.Close()
		return nil, nil, errors.Join(err, derr)
	}
	release := func() error {
		derr := deleteStream(broker.Conn(), "courier_"+suffix)
		ctx, cancel := context.WithTimeout(context.Background(), natsWait)
		defer cancel()
		return errors.Join(derr, broker.Shutdown(ctx))
	}
	return broker, release, nil
}

// natsConnect connects to the NATS server at the URL, waiting at most
// natsWait.
func (i *Infrastructure) natsConnect() (*natsgo.Conn, error) {
	url := i.cfg.natsURL()
	if url == "" {
		return nil, errors.New("no NATS URL: set --nats-url or MESSAGING_NATS_URL")
	}
	nc, err := natsgo.Connect(url, natsgo.Name("courier"), natsgo.Timeout(natsWait))
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", url, err)
	}
	return nc, nil
}

// deleteStream deletes the named stream on nc; a stream that does not exist
// is already gone.
func deleteStream(nc *natsgo.Conn, name string) error {
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), natsWait)
	defer cancel()
	if err := js.DeleteStream(ctx, name); err != nil && !errors.Is(err, jetstream.ErrStreamNotFound) {
		return fmt.Errorf("delete stream %s: %w", name, err)
	}
	return nil
}

// Needs returns what the broker requires to run, which each scenario checks
// first. It reads the flags, so it is called once they are parsed. The
// memory broker needs nothing; the nats broker needs a NATS server with
// JetStream enabled at the URL.
func (i *Infrastructure) Needs() []scenario.Need {
	if i.cfg.Broker != "nats" {
		return nil
	}
	return []scenario.Need{{What: "NATS with JetStream at --nats-url or $MESSAGING_NATS_URL", Check: i.natsPing}}
}

func (i *Infrastructure) natsPing(ctx context.Context) error {
	nc, err := i.natsConnect()
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, natsWait)
	defer cancel()
	if _, err := js.AccountInfo(ctx); err != nil {
		return fmt.Errorf("JetStream: %w", err)
	}
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
