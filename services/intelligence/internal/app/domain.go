package app

import (
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"

	"github.com/JaimeStill/spike-messaging/services/intelligence/domain/intelligence"
	"github.com/JaimeStill/spike-messaging/services/intelligence/internal/config"
)

// Domain composes the application's domain services, one field per domain
// layer. Intelligence fuses each faction's observations into its
// assessment.
type Domain struct {
	Intelligence *intelligence.Service
}

// newDomain wires the domain layer over infra: each domain package's service
// is constructed here from the infrastructure fields it uses, never the
// Infrastructure struct itself. It takes cfg for the settings a domain is
// built with, and lc because a domain verifies its statements against the
// migrated schema at verifyStage, after the schema service at admin.Stage
// has corrected it.
func newDomain(infra *Infrastructure, cfg *config.Config, lc *lifecycle.Coordinator) *Domain {
	intel := intelligence.New(infra.SQL, infra.Messaging.Recorder, cfg.Intelligence.ContactRounds)
	lc.Add(lifecycle.Service{Name: "intelligence", Stage: verifyStage, Start: intel.Verify})
	return &Domain{Intelligence: intel}
}

// mountAPI builds the API mount, /api, with each domain layer's route group
// mounted into it.
func mountAPI(dom *Domain, _ *config.Config) *web.Group {
	g := web.NewGroup("/api")
	g.Mount(intelligence.Routes(dom.Intelligence))
	return g
}
