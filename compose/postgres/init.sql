-- The databases of the exercise's four services, one each on the compose
-- Postgres: the environment provisions a service's database, and the
-- service owns only its schema, which its admin stage migrates. The
-- entrypoint runs this file once, when the volume is first initialized, so
-- an existing volume needs `mise run reset` to gain the databases.
CREATE DATABASE exercise;
CREATE DATABASE intelligence;
CREATE DATABASE command;
CREATE DATABASE operations;
