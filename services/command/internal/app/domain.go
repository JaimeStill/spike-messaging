package app

import (
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"

	"github.com/JaimeStill/spike-messaging/services/command/domain/command"
	"github.com/JaimeStill/spike-messaging/services/command/internal/config"
)

// Domain composes the application's domain services, one field per domain
// layer. Command decides on each faction's assessments and directs its
// elements.
type Domain struct {
	Command *command.Service
}

// newDomain wires the domain layer over infra: each domain package's service
// is constructed here from the infrastructure fields it uses, never the
// Infrastructure struct itself. It takes lc because a domain verifies its
// statements against the migrated schema at verifyStage, after the schema
// service at admin.Stage has corrected it.
func newDomain(infra *Infrastructure, lc *lifecycle.Coordinator) *Domain {
	cmd := command.New(infra.SQL, infra.Messaging.Recorder)
	lc.Add(lifecycle.Service{Name: "command", Stage: verifyStage, Start: cmd.Verify})
	return &Domain{Command: cmd}
}

// mountAPI builds the API mount, /api, with each domain layer's route group
// mounted into it.
func mountAPI(dom *Domain, _ *config.Config) *web.Group {
	g := web.NewGroup("/api")
	g.Mount(command.Routes(dom.Command))
	return g
}
