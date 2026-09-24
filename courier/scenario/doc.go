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
package scenario
