-- One faction's assessment in one exercise: what the faction knows, as a
-- fusion.Picture in JSON. The picture holds the faction's own elements, the
-- contacts it knows of with the round each was last seen, every objective
-- of the map with its last-seen holder, and the round it is of, -1 before
-- the first. A closed assessment takes no more observations.
CREATE TABLE assessment (
    exercise_id uuid        NOT NULL,
    faction     text        NOT NULL,
    status      text        NOT NULL DEFAULT 'open',
    picture     jsonb       NOT NULL,
    opened_at   timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_assessment PRIMARY KEY (exercise_id, faction),
    CONSTRAINT cc_assessment_status CHECK (status IN ('open', 'closed'))
);
