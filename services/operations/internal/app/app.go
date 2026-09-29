package app

import (
	"context"
	"io"
	"log/slog"

	"github.com/JaimeStill/spike-messaging/services/operations/internal/config"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"
)

// App is the application layer: it assembles infrastructure, the admin
// layer, the domain, and the reactors into a router and a lifecycle
// coordinator, and runs the process.
type App struct {
	cfg    *config.Config
	logger *slog.Logger
	lc     *lifecycle.Coordinator
	server *web.Server
}

// New assembles the application from cfg and a writer for its logger:
// infrastructure, the admin layer, the domain, and the reactors, then the
// router with its middleware and mounts, and the server as the
// coordinator's root-stage service. It performs no I/O and returns before
// Run starts the coordinator.
func New(cfg *config.Config, w io.Writer) (*App, error) {
	lc := lifecycle.New()

	infra, err := newInfrastructure(w, cfg, lc)
	if err != nil {
		return nil, err
	}

	adm, err := newAdmin(infra, cfg, lc)
	if err != nil {
		return nil, err
	}

	dom := newDomain(infra, lc)

	if _, err := newReactors(infra, dom, cfg, lc); err != nil {
		return nil, err
	}

	router := web.NewRouter()
	router.Use(middleware(infra)...)
	for _, m := range routes(dom, adm, cfg) {
		router.Mount(m)
	}

	server := web.NewServer(cfg.Server, router)
	lc.Add(lifecycle.Service{
		Name:     "server",
		Stage:    lifecycle.StageRoot,
		Start:    server.Start,
		Shutdown: server.Shutdown,
	})
	lc.Monitor(server.Err())

	// The zero Problem keeps the SDK's readiness defaults: type about:blank,
	// status 503 with its status text as the title, the generic detail, and
	// the checks extension member. A generated service names its own
	// not-ready problem type here once it has one.
	web.RegisterHealth(router, lc, web.Problem{})

	lc.OnReady(func() {
		infra.Logger.Info("server ready", "addr", server.Addr())
	})

	return &App{
		cfg:    cfg,
		logger: infra.Logger,
		lc:     lc,
		server: server,
	}, nil
}

// Run starts the lifecycle coordinator and blocks until it drains, using
// cfg's shutdown timeout. It logs and returns 1 on failure, or logs "server
// stopped" and returns 0.
func (a *App) Run(ctx context.Context) int {
	if err := a.lc.Run(ctx, a.cfg.ShutdownTimeout.Duration()); err != nil {
		a.logger.Error("service failed", "error", err)
		return 1
	}
	a.logger.Info("server stopped")
	return 0
}
