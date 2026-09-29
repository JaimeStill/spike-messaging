package app

import (
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"

	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise"
	"github.com/JaimeStill/spike-messaging/services/exercise/internal/config"
)

// Domain composes the application's domain services, one field per domain
// layer. Exercise is the world: exercises, their rounds, and the orders
// recorded for them.
type Domain struct {
	Exercise *exercise.Service
}

// newDomain wires the domain layer over infra: each domain package's service
// is constructed here from the infrastructure fields it uses, never the
// Infrastructure struct itself. It takes lc because a domain verifies its
// statements against the migrated schema at verifyStage, after the schema
// service at admin.Stage has corrected it.
func newDomain(infra *Infrastructure, lc *lifecycle.Coordinator) *Domain {
	ex := exercise.New(infra.SQL, infra.Messaging.Recorder)
	lc.Add(lifecycle.Service{Name: "exercise", Stage: verifyStage, Start: ex.Verify})
	return &Domain{Exercise: ex}
}

// mountAPI builds the API mount, /api, with each domain layer's route group
// mounted into it.
func mountAPI(dom *Domain, _ *config.Config) *web.Group {
	g := web.NewGroup("/api")
	g.Mount(exercise.Routes(dom.Exercise))
	return g
}
