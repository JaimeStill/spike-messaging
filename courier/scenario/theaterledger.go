package scenario

import (
	"slices"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
)

// factionRound names one faction's events of one round.
type factionRound struct {
	faction string
	round   int
}

// ledger is the theater's record of the stream's traffic: the count of the
// exercise's events by type, and the times of the chain's events, by type,
// faction, and round, for the chain's latency.
type ledger struct {
	counts map[string]int                          // events by type
	times  map[string]map[factionRound][]time.Time // event times, by type, faction, and round
}

func newLedger() ledger {
	return ledger{counts: map[string]int{}, times: map[string]map[factionRound][]time.Time{}}
}

// stamp records e's time under its type, its faction, and its round.
func (l *ledger) stamp(e event.Event, faction string, round int) {
	if l.times[e.Type] == nil {
		l.times[e.Type] = map[factionRound][]time.Time{}
	}
	k := factionRound{faction, round}
	l.times[e.Type][k] = append(l.times[e.Type][k], e.Time)
}

// hops returns, for each faction and round with an event of type from, the
// time from that event to the first later event of type to for the same
// faction in the round shift rounds on. A round with no event of type to
// contributes nothing, so assessed -> directed measures only rounds that led
// to a directive. A round's revised assessment follows its first, so
// observed -> assessed measures the first assessment, and the revision
// counts toward assessed -> directed only when a directive follows it.
func (l *ledger) hops(from, to string, shift int) []time.Duration {
	var out []time.Duration
	for k, starts := range l.times[from] {
		ends := l.times[to][factionRound{k.faction, k.round + shift}]
		for _, s := range starts {
			var best time.Duration = -1
			for _, e := range ends {
				if d := e.Sub(s); d >= 0 && (best < 0 || d < best) {
					best = d
				}
			}
			if best >= 0 {
				out = append(out, best)
			}
		}
	}
	return out
}

// p50 renders the median of ds, or "none" when it is empty.
func p50(ds []time.Duration) string {
	if len(ds) == 0 {
		return "none"
	}
	slices.Sort(ds)
	return ds[len(ds)/2].Round(time.Millisecond).String()
}
