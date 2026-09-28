--| tier: native
--| transaction: required
--| native: INSERT ... ON CONFLICT DO UPDATE, so a faction's later orders for a round replace its earlier ones in one statement. Port: MERGE (SQL Server, Oracle), INSERT ... ON DUPLICATE KEY UPDATE (MySQL), ON CONFLICT DO UPDATE (SQLite).
-- Records a faction's orders for a round, a list of rules.Order as JSON,
-- replacing any it recorded before for the same round: the last wins.
INSERT INTO exercise_orders (exercise_id, faction, round, orders)
VALUES ({{exercise_id:uuid}}, {{faction}}, {{round:int}}, {{orders}})
ON CONFLICT (exercise_id, faction, round)
DO UPDATE SET orders = EXCLUDED.orders, recorded_at = CURRENT_TIMESTAMP
