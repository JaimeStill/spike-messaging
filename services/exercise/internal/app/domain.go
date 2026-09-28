package app

import (
	"github.com/JaimeStill/spike-messaging/services/exercise/internal/config"
	"github.com/standards-lab/go-web-sdk"
)

// Domain composes the application's domain services, one field per domain
// layer. Domain services are defined in the module's base-layer domain
// packages, one package per layer under domain/, and constructed here from
// the infrastructure fields they depend on. The template ships it empty;
// which services it composes is the application author's decision.
type Domain struct{}

// newDomain wires the domain layer over infra: each domain package's service
// is constructed here from the infrastructure fields it uses, never the
// Infrastructure struct itself. It takes no lifecycle coordinator: domain
// services own no resource and never run, so there is nothing in scope to
// register. A package that does own a resource and run belongs in
// infrastructure.go, if it is built once at startup, or reactors.go, if it
// reacts to an external occurrence for the life of the process.
func newDomain(infra *Infrastructure) *Domain {
	return &Domain{}
}

// mountAPI builds the API mount, /api, with each domain layer's route group
// mounted into it, each handler handed its policy from cfg at the
// construction site (cfg.Reads.Limits() for a collection read). The
// template ships the group initialized and empty.
func mountAPI(dom *Domain, cfg *config.Config) *web.Group {
	return web.NewGroup("/api")
}
