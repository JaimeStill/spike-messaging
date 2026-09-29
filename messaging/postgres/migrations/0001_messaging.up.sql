-- The outbox: one row per emitted event, inserted in the transaction that
-- makes the state the event reports true. seq orders the relay's publishing,
-- and published_at stays NULL until the broker holds the event. The event is
-- stored in binary content mode: header holds the ce- attributes, data the
-- body. (source, id) is the event's identity, so emitting one event twice
-- fails the emitting transaction through messaging_uq_outbox_event.
CREATE TABLE messaging_outbox (
    seq          bigint      GENERATED ALWAYS AS IDENTITY,
    source       text        NOT NULL,
    id           text        NOT NULL,
    header       jsonb       NOT NULL,
    data         bytea,
    created_at   timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    CONSTRAINT messaging_pk_outbox PRIMARY KEY (seq),
    CONSTRAINT messaging_uq_outbox_event UNIQUE (source, id)
);

-- The relay's pass reads only the unpublished rows, in seq order.
CREATE INDEX messaging_ix_outbox_unpublished ON messaging_outbox (seq)
    WHERE published_at IS NULL;

-- The inbox: one row per event a consumer has handled, inserted in the
-- handler's own transaction, so a redelivered event finds its row and is
-- skipped. consumer names the handler, typically its subscription's Name.
CREATE TABLE messaging_inbox (
    consumer   text        NOT NULL,
    source     text        NOT NULL,
    id         text        NOT NULL,
    handled_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT messaging_pk_inbox PRIMARY KEY (consumer, source, id)
);
