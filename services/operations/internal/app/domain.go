package app

import (
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"

	"github.com/JaimeStill/spike-messaging/services/operations/domain/operations"
	"github.com/JaimeStill/spike-messaging/services/operations/internal/config"
)

// Domain composes the application's domain services, one field per domain
// layer. Operations maneuvers each faction's elements toward the targets
// its directives set.
type Domain struct {
	Operations *operations.Service
}

// newDomain wires the domain layer over infra: each domain package's service
// is constructed here from the infrastructure fields it uses, never the
// Infrastructure struct itself. It takes lc because a domain verifies its
// statements against the migrated schema at verifyStage, after the schema
// service at admin.Stage has corrected it.
func newDomain(infra *Infrastructure, lc *lifecycle.Coordinator) *Domain {
	ops := operations.New(infra.SQL, infra.Messaging.Recorder)
	lc.Add(lifecycle.Service{Name: "operations", Stage: verifyStage, Start: ops.Verify})
	return &Domain{Operations: ops}
}

// mountAPI builds the API mount, /api, with each domain layer's route group
// mounted into it.
func mountAPI(dom *Domain, _ *config.Config) *web.Group {
	g := web.NewGroup("/api")
	g.Mount(operations.Routes(dom.Operations))
	return g
}
