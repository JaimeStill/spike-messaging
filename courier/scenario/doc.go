// Package scenario holds courier's narrated scenarios and the runner that
// drives them. Each scenario shows one capability of the event and reactor
// layer on a broker the composition root supplies: it validates its flags,
// checks what it needs, then runs its steps, and each step says what it is
// about to do before doing it and reports what it observed.
//
// A scenario that runs reactors starts go-core's lifecycle coordinator in
// one step and signals its drain in a later one, so the scenario ends on its
// own; an interrupt drains it the same way.
//
// The outbox scenario also takes an [OutboxStore] from the composition root:
// an outbox over a scratch database, with the engine's SQL behind it. The
// package itself imports no engine and holds no SQL.
//
// The request scenario takes an [Exchange] the same way: a native request
// and reply the composition root builds on the provider's own client, with a
// responder that runs as a reactor. The package imports no provider.
//
// The directives scenario stands in for the exercise's command service. It
// takes [Joins], a broker on the stream the exercise services share rather
// than a scratch one, reads a faction's first observation from it, and
// issues one command.directive.issued that sends each of the faction's
// elements to an objective the observation reports, or holds it when the
// faction knows none.
//
// The assessments scenario joins the same stream and narrates one
// exercise's intelligence.assessment.issued events and the
// command.directive.issued events decided on them, until the exercise
// concludes.
//
// The theater scenario is the demonstration. It joins the same stream
// before an exercise starts and narrates the exercise as the services play
// it: the initial conditions, with the seed and the objectives (which the
// factions do not know, and which the narration names objective:x,y), both
// read from exercise's API at the start; each round as a block,
// told once an event of a later round arrives, by side: what the observer
// (exercise, the umpire) recorded of the round, then what each faction
// knows, decides, and orders in it, with a change that arrives after its
// round's block told late in the next; and the final conditions, with the
// events on the stream by type and the median latency of each hop in the
// chain.
//
// The theater-check scenario reconciles a run afterward. It joins the same
// stream under a new durable, which reads it from its beginning, collects
// the exercise's assessments, and checks each against exercise's history,
// the observer's record, which it reads over exercise's HTTP API: what the
// faction truly had and saw that round, under intelligence's suppression
// rules. It fails on any inconsistency.
package scenario
