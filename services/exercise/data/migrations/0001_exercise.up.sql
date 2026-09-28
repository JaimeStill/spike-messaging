-- An exercise: its settings, its status, the last round resolved, and the
-- world as that round left it. state is a rules.State as JSON, the elements
-- included; verdict is a rules.Verdict, null until the exercise concludes or
-- is stopped. next_round_at is when the next round is due, set only while
-- the exercise runs, and always computed from the database's clock, so
-- replicas agree on what is due.
CREATE TABLE exercise (
    id                uuid        PRIMARY KEY DEFAULT uuidv7(),
    name              text        NOT NULL,
    status            text        NOT NULL,
    round_interval_ms bigint      NOT NULL,
    round_limit       int         NOT NULL,
    round             int         NOT NULL DEFAULT 0,
    state             jsonb       NOT NULL,
    verdict           jsonb,
    next_round_at     timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT cc_exercise_status CHECK (status IN ('created', 'running', 'paused', 'concluded', 'stopped')),
    CONSTRAINT cc_exercise_round_interval CHECK (round_interval_ms > 0),
    CONSTRAINT cc_exercise_round_limit CHECK (round_limit > 0)
);

-- The resolver's pass reads only the running exercises, by when each is due.
CREATE INDEX ix_exercise_due ON exercise (next_round_at)
    WHERE status = 'running';

-- The round history: the state, both factions' observations, and the
-- verdict after each round, round 0 written when the exercise starts.
CREATE TABLE exercise_round (
    exercise_id  uuid        NOT NULL REFERENCES exercise (id) ON DELETE CASCADE,
    round        int         NOT NULL,
    state        jsonb       NOT NULL,
    observations jsonb       NOT NULL,
    verdict      jsonb       NOT NULL,
    resolved_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_exercise_round PRIMARY KEY (exercise_id, round)
);

-- A faction's orders for a round, as operations last issued them: a later
-- record for the same exercise, faction, and round replaces the orders.
CREATE TABLE exercise_orders (
    exercise_id uuid        NOT NULL REFERENCES exercise (id) ON DELETE CASCADE,
    faction     text        NOT NULL,
    round       int         NOT NULL,
    orders      jsonb       NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_exercise_orders PRIMARY KEY (exercise_id, faction, round)
);
