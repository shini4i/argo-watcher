-- Shared deploy lock state, so a lockdown set through the API applies to every
-- replica instead of only the one that served the request.
-- The table holds exactly one row: the CHECK constraint on the primary key makes
-- a second row impossible, which keeps reads free of "which row is current?".
CREATE TABLE IF NOT EXISTS deploy_lock
(
    id             INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    manual_lock    BOOLEAN NOT NULL DEFAULT false,
    -- Deadline of a temporary override of a scheduled lockdown; NULL when none
    -- is active. A deadline rather than a flag so the override expires
    -- consistently across replicas and restarts.
    override_until TIMESTAMPTZ
);

INSERT INTO deploy_lock (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
