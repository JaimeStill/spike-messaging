package app

import (
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"

	"github.com/JaimeStill/spike-messaging/services/intelligence/internal/config"
)

// Domain composes the application's domain services, one field per domain
// layer.
type Domain struct{}

// newDomain wires the domain layer over infra: each domain package's service
// is constructed here from the infrastructure fields it uses, never the
// Infrastructure struct itself. It takes lc because a domain verifies its
// statements against the migrated schema at verifyStage, after the schema
// service at admin.Stage has corrected it.
func newDomain(_ *Infrastructure, _ *lifecycle.Coordinator) *Domain {
	return &Domain{}
}

// mountAPI builds the API mount, /api, with each domain layer's route group
// mounted into it.
func mountAPI(_ *Domain, _ *config.Config) *web.Group {
	return web.NewGroup("/api")
}
