// Package integration is the service's integration tier: the harness that
// runs the composed service as the binary and drives it through its
// production seams, and, under the integration build tag, the suite that
// asserts the service's behavior through its API.
//
// The harness is the toolkit the SDKs ship beside what it exercises:
// go-core's processtest builds cmd/server once per run ([Main]), runs it as
// a subprocess configured by EXERCISE_* environment variables on a reserved
// port, and reads its exit code; go-web-sdk's webtest drives it through its
// HTTP surface and observes its liveness probe. [Start] runs the service
// and returns once it is live; [Service.Stop] interrupts it and returns
// the exit code. Nothing in the runtime exists for the tests' sake.
//
// The suite files, tagged integration, run the built service; the harness
// itself is untagged, so the unit tier type-checks it on every pull
// request. A service that adds a backing service runs the suite against
// its compose stack as an isolated project, the pattern the reference
// service proves; the template is engine-free and needs none.
package integration
