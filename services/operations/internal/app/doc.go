// Package app is the composition root, laid out as one file per layer of
// the architecture:
//
//   - infrastructure.go constructs the services the application is composed on
//   - admin.go constructs the administrative services and their /admin mount
//   - domain.go constructs the domain services and their /api mount
//   - reactors.go constructs the event-driven entry points
//
// routes.go is the list of mounts, and middleware.go declares the
// router-level middleware stack, outermost first. Each layer file
// constructs its layer and owns its mount, so the package's file list is
// the architecture's layer list. Extending the service means editing a
// layer file's body; the signatures, cmd/server, and [App.Run] stay
// untouched.
//
// [New] is the cold start, with no I/O: it constructs infrastructure (each
// service registering on the coordinator where it is constructed), the
// admin layer over it, the domain over it, and the reactors over both, then
// assembles the router from the mounts and the middleware stack. It then
// declares the server as the coordinator's root-stage service, started
// after every infrastructure stage and drained first, so in-flight requests
// complete before the infrastructure beneath them closes. The probes
// register on the router's native mux, outside every module's middleware,
// and query the coordinator live on every request, aggregating it under the
// "lifecycle" name ahead of its services' own checks. Wiring mistakes panic
// at construction.
//
// Routes and reactors are the two ways a domain service enters the running
// process: a route is driven by a caller, a reactor by an occurrence the
// process receives or discovers. Both take *Domain; neither is a domain
// service itself.
//
// [App.Run] is the hot start plus shutdown, delegated to the coordinator,
// and returns the process exit code.
package app
