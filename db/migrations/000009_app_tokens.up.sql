-- Application deploy tokens: opaque credentials, each authorizing a git
-- write-back for a named set of applications, issued and revoked through the API.
--
-- Only the SHA-256 of a token is stored, so a database dump does not hand out
-- working credentials. The token is 256 bits of CSPRNG output, which is why a
-- plain digest is enough and a password KDF would only add cost per request.
CREATE TABLE IF NOT EXISTS app_tokens
(
    id           UUID PRIMARY KEY      DEFAULT gen_random_uuid(),
    token_hash   BYTEA        NOT NULL UNIQUE,
    -- The applications this token covers, or an empty array when all_apps is set.
    -- JSONB rather than TEXT[] to match how the tasks table already stores lists;
    -- nothing queries inside it, because scope matching happens in Go once the
    -- row has been found by its hash.
    apps         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    -- A wildcard covering every application, present and future. It is a column
    -- of its own rather than a sentinel entry in apps, so granting the whole
    -- estate can never be the accidental result of building that list wrongly.
    all_apps     BOOLEAN      NOT NULL DEFAULT false,
    -- The token's last few characters, so the UI can name a token whose secret it
    -- can never show again.
    hint         VARCHAR(8)   NOT NULL,
    description  VARCHAR(255) NOT NULL DEFAULT '',
    created_by   VARCHAR(255) NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    -- NULL means the token never expires.
    expires_at   TIMESTAMPTZ,
    -- NULL while the token is live. A revoked token keeps its row, so who issued
    -- it and when it was withdrawn stay on record.
    revoked_at   TIMESTAMPTZ,
    -- Recorded only when the token authorizes a deployment, never on a read, so a
    -- status endpoint polled for the length of every rollout is not turned into a
    -- stream of writes. It is the signal for pruning tokens nothing uses.
    last_used_at TIMESTAMPTZ,
    -- jsonb_typeof is checked first so the constraint fails rather than evaluating to
    -- NULL (which a CHECK passes) if a writer ever stores JSON null instead of [].
    CONSTRAINT app_tokens_scope_exclusive CHECK (
        jsonb_typeof(apps) = 'array'
            AND ((all_apps AND jsonb_array_length(apps) = 0)
                OR (NOT all_apps AND jsonb_array_length(apps) > 0))
        )
);
