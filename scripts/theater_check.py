#!/usr/bin/env python3
"""Reconcile a theater run's assessments against the umpire's record.

For every assessment intelligence issued of the exercise, it checks the
suppression rules against the true state of that round, which exercise's
history API holds:

  - own:        the faction's own elements are exactly the true ones;
  - sees:       every enemy element in sight is a contact of age 0, at its
                true cell with its true strength, and no other contact is;
  - remembers:  every older contact's cell is out of sight, and it is no
                older than contact_rounds;
  - objectives: every objective in sight shows its true holder, no
                objective seen before is unknown, and none out of sight is
                reported as seen this round;
  - coverage:   every round has one assessment per faction.

It reports every inconsistency and exits 1 on any, then reports each
faction's stale beliefs at the end, what it last saw of an objective against
who truly holds it, and the verdict.

Usage: theater_check.py [exercise-id]   (default: the latest exercise)
It reads exercise's API on EXERCISE_URL (default http://localhost:8081), the
intelligence database through the compose stack's Postgres, and
contact_rounds from INTELLIGENCE_CONTACT_ROUNDS (default 3).
"""

import json
import os
import subprocess
import sys
import urllib.request
import uuid

SIGHT = {"force": 2, "scout": 4}
K = int(os.environ.get("INTELLIGENCE_CONTACT_ROUNDS", "3"))
URL = os.environ.get("EXERCISE_URL", "http://localhost:8081")


def psql(db, sql):
    out = subprocess.run(
        ["docker", "compose", "exec", "-T", "postgres", "psql", "-U", "app", "-d", db, "-Atc", sql],
        check=True, capture_output=True, text=True,
    ).stdout
    return [line for line in out.splitlines() if line]


def get(path):
    with urllib.request.urlopen(URL + "/api/exercises/" + path) as r:
        return json.load(r)


def place(at):
    return f'{at["sector"]}:{at["x"]},{at["y"]}'


def sees(own, at):
    return any(
        e["at"]["sector"] == at["sector"]
        and max(abs(e["at"]["x"] - at["x"]), abs(e["at"]["y"] - at["y"])) <= SIGHT[e["kind"]]
        for e in own
    )


def main():
    if len(sys.argv) > 1:
        try:
            ex = str(uuid.UUID(sys.argv[1]))
        except ValueError:
            sys.exit(f"{sys.argv[1]!r} is not an exercise ID")
    else:
        rows = psql("exercise", "select id from exercise order by created_at desc limit 1")
        if not rows:
            sys.exit("no exercise found; run mise run demo-theater first")
        ex = rows[0]
    history = {h["round"]: h["state"] for h in get(ex + "/history")}
    summary = get(ex)
    assessments = [
        json.loads(row)
        for row in psql(
            "intelligence",
            "select convert_from(data, 'UTF8') from messaging_outbox "
            f"where convert_from(data, 'UTF8')::jsonb->>'exercise' = '{ex}' order by seq",
        )
    ]
    assessments.sort(key=lambda a: (a["round"], a["faction"]))

    errors, seen_before, last = [], {}, {}
    factions = history[min(history)]["factions"]
    issued = {(a["round"], a["faction"]) for a in assessments}
    for r in sorted(history):
        for f in factions:
            if (r, f) not in issued:
                errors.append(f"round {r} {f}: no assessment was issued")
    for a in assessments:
        r, f = a["round"], a["faction"]
        if r not in history:
            errors.append(f"round {r} {f}: assessed, but exercise's history has no such round")
            continue
        state = history[r]
        tag = f"round {r} {f}"
        own = [e for e in state["elements"] if e["faction"] == f]
        key = lambda e: (e["id"], e["strength"], place(e["at"]))
        if sorted(map(key, a["own"])) != sorted(map(key, own)):
            errors.append(f"{tag}: own elements differ from the truth")
        visible = {e["id"]: e for e in state["elements"] if e["faction"] != f and sees(own, e["at"])}
        contacts = {c["id"]: c for c in a["contacts"]}
        for i, e in visible.items():
            c = contacts.get(i)
            if not c or c["age"] != 0 or key(c) != key(e):
                errors.append(f"{tag}: {i} is in sight at {place(e['at'])}, strength {e['strength']}, but not reported so")
        for i, c in contacts.items():
            if c["age"] == 0 and i not in visible:
                errors.append(f"{tag}: {i} is reported in sight, but is not")
            if c["age"] > 0 and sees(own, c["at"]):
                errors.append(f"{tag}: {i} is remembered at {place(c['at'])}, a cell in sight")
            if c["age"] > K:
                errors.append(f"{tag}: {i} is remembered {c['age']} rounds, past {K}")
        holders = state.get("holders") or {}
        for o in a["objectives"]:
            at = place(o["at"])
            if sees(own, o["at"]):
                seen_before.setdefault(f, set()).add(at)
                if not o["known"] or o["age"] != 0 or o["holder"] != holders.get(at, ""):
                    errors.append(f"{tag}: objective {at} is in sight, held by {holders.get(at) or 'none'}, but not reported so")
            elif o["known"] and o["age"] == 0:
                errors.append(f"{tag}: objective {at} is reported seen this round, but is out of sight")
            elif not o["known"] and at in seen_before.get(f, set()):
                errors.append(f"{tag}: objective {at} is unknown, but was seen before")
        last[f] = a

    rounds = max(history) + 1
    print(f"theater {ex}: {rounds} rounds, {len(assessments)} assessments, contact_rounds {K}")
    if errors:
        print(f"{len(errors)} inconsistencies:")
        for e in errors:
            print("  " + e)
    else:
        print("consistent: every assessment matches the umpire's record under the suppression rules")

    truth = history[max(history)].get("holders") or {}
    print("\nwhat each faction believes at the end, against the truth:")
    width = max(len(f) for f in last)
    for f, a in sorted(last.items()):
        for o in a["objectives"]:
            at = place(o["at"])
            believed = (o["holder"] or "unheld") if o["known"] else "unknown"
            actual = truth.get(at) or "unheld"
            if believed != actual:
                since = f", seen {o['age']} rounds ago" if o["known"] else ""
                print(f"  {f:<{width}}  {at:<14} believes {believed}{since}; truly {actual}")
    held = {}
    for at, h in sorted(truth.items()):
        held.setdefault(h, []).append(at)
    v = summary.get("verdict") or {}
    print(f"\nverdict: {v.get('winner') or 'no winner'} by {v.get('reason')}; truly held: "
          + "; ".join(f"{h} {len(ats)} ({' '.join(ats)})" for h, ats in sorted(held.items())))
    sys.exit(1 if errors else 0)


if __name__ == "__main__":
    main()
