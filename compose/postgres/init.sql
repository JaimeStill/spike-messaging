-- Creates one database on the compose Postgres for each of the exercise's
-- four services. The environment provisions a service's database; the
-- service owns only its schema, which its admin stage migrates. The
-- entrypoint runs this file once, when the volume is first initialized, so
-- an existing volume gains the databases only after `mise run reset`.
CREATE DATABASE exercise;
CREATE DATABASE intelligence;
CREATE DATABASE command;
CREATE DATABASE operations;
