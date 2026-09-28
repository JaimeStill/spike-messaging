package app

import (
	"fmt"

	"github.com/JaimeStill/spike-messaging/services/exercise/internal/config"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate/migrate"
)

// Admin composes the administrative services, one field per admin domain:
// the administrative counterpart of Domain, each service administering one
// infrastructure service over the library mechanisms it triggers. Database
// is the schema service over the migration sets.
type Admin struct {
	Database *admin.Service
}

// newAdmin wires the admin layer over infra, each admin service handed its
// switches from cfg at the construction site. It takes lc because an admin
// service owns a lifecycle stage: one that verifies and corrects the state
// of the infrastructure it administers registers here, ahead of the domains
// that depend on that state: the database admin service migrates the
// messaging set and the service's own at stage 1, ahead of the statements
// verified at stage 2.
func newAdmin(
	infra *Infrastructure,
	_ *config.Config,
	lc *lifecycle.Coordinator,
) (*Admin, error) {
	migrator, err := migrate.New(infra.SQL.DB, infra.Sets, migrate.Options{Logger: infra.Logger})
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	db := admin.New(infra.DB, infra.SQL.DB, migrator, infra.SQL.Catalog, admin.Options{Logger: infra.Logger})
	db.Register(lc)
	return &Admin{Database: db}, nil
}

// mountAdmin builds the admin mount, /admin, with each admin domain's route
// group mounted into it. The template ships the group initialized and empty.
// In production the mount belongs on its own listener, authenticated and
// unreachable from the public API's network path; that isolation is a
// design constraint the application settles when the first admin service
// arrives.
func mountAdmin(adm *Admin) *web.Group {
	return web.NewGroup("/admin")
}
