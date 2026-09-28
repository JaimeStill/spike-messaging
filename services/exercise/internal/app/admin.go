package app

import (
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"
	"github.com/JaimeStill/spike-messaging/services/exercise/internal/config"
)

// Admin composes the administrative services, one field per admin domain:
// the administrative counterpart of Domain, each service administering one
// infrastructure service over the library mechanisms it triggers. The
// template ships it empty; which services it composes follows the
// infrastructure the application adopts.
type Admin struct{}

// newAdmin wires the admin layer over infra, each admin service handed its
// switches from cfg at the construction site. It takes lc because an admin
// service owns a lifecycle stage: one that verifies and corrects the state
// of the infrastructure it administers registers here, ahead of the domains
// that depend on that state.
func newAdmin(
	infra *Infrastructure,
	cfg *config.Config,
	lc *lifecycle.Coordinator,
) (*Admin, error) {
	return &Admin{}, nil
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
