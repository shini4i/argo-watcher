CREATE TABLE IF NOT EXISTS tasks
(
    id      VARCHAR(36)  NOT NULL PRIMARY KEY,
    created TIMESTAMP    NOT NULL,
    updated TIMESTAMP    DEFAULT NULL,
    images  JSON         NOT NULL,
    status  VARCHAR(20)  NOT NULL,
    app     VARCHAR(255) DEFAULT NULL,
    author  VARCHAR(255) DEFAULT NULL,
    project VARCHAR(255) DEFAULT NULL
);
CREATE INDEX IF NOT EXISTS tasks_idx_created ON tasks (created);