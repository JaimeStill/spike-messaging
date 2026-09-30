-- One faction's direction in one exercise. It holds the public map the
-- faction decides over, as a decide.Map in JSON; the last round it decided
-- on, -1 before the first; and the decision standing for each of its live
-- elements, as decide.Decisions in JSON. A closed direction decides nothing
-- past closed_round, the round its exercise concluded after: the final
-- round's assessments and the conclusion are raised apart and consumed
-- apart.
CREATE TABLE direction (
    exercise_id  uuid        NOT NULL,
    faction      text        NOT NULL,
    status       text        NOT NULL DEFAULT 'open',
    map          jsonb       NOT NULL,
    round        int         NOT NULL DEFAULT -1,
    decisions    jsonb       NOT NULL DEFAULT '[]',
    closed_round int,
    opened_at    timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_direction PRIMARY KEY (exercise_id, faction),
    CONSTRAINT cc_direction_status CHECK (status IN ('open', 'closed'))
);
