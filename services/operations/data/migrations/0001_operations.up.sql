-- One faction's operation in one exercise: the public map it plans over,
-- the faction's elements as its last observation left them, and the target
-- each element's directive set. elements is a list of route.Element as
-- JSON, and targets maps an element's ID to a route.Location. last_round is
-- the last observed round operations issued orders from, -1 before the
-- first. round_limit is the exercise's, past which exercise refuses
-- orders. A closed operation acts on nothing more.
CREATE TABLE operation (
    exercise_id uuid        NOT NULL,
    faction     text        NOT NULL,
    status      text        NOT NULL DEFAULT 'open',
    map         jsonb       NOT NULL,
    round_limit int         NOT NULL,
    last_round  int         NOT NULL DEFAULT -1,
    elements    jsonb       NOT NULL DEFAULT '[]',
    targets     jsonb       NOT NULL DEFAULT '{}',
    opened_at   timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_operation PRIMARY KEY (exercise_id, faction),
    CONSTRAINT cc_operation_status CHECK (status IN ('open', 'closed'))
);
