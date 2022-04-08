-- migrate:up
CREATE TABLE tasks
(
    id      VARCHAR(36) NOT NULL PRIMARY KEY,
    created TIMESTAMP   NOT NULL,
    updated TIMESTAMP    DEFAULT NULL,
    images  JSON        NOT NULL,
    status  VARCHAR(20) NOT NULL,
    app     VARCHAR(255) DEFAULT NULL,
    author  VARCHAR(255) DEFAULT NULL
);
CREATE INDEX tasks_idx_created ON tasks (created);

-- migrate:down
