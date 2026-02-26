package sql

// SQLITE SCHEMAS

const SQLITE_SCHEMA = `
CREATE TABLE IF NOT EXISTS evaluations (
    id VARCHAR(36) NOT NULL,
    resource TEXT NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    experiment_id VARCHAR(255) NOT NULL,
    entity TEXT NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS collections (
    id VARCHAR(36) NOT NULL,
    resource TEXT NOT NULL,
    entity TEXT NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS providers (
    id VARCHAR(36) NOT NULL,
    resource TEXT NOT NULL,
    entity TEXT NOT NULL,
    PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS idx_eval_entity
ON evaluations (id);

CREATE INDEX IF NOT EXISTS idx_collection_entity
ON collections (id);

CREATE INDEX IF NOT EXISTS idx_provider_entity
ON providers (id);
`

// POSTGRES SCHEMAS

const POSTGRES_SCHEMA = `
CREATE TABLE IF NOT EXISTS evaluations (
    id VARCHAR(36) NOT NULL,
    resource JSONB NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    experiment_id VARCHAR(255) NOT NULL,
    entity JSONB NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS collections (
    id VARCHAR(36) NOT NULL,
    resource JSONB NOT NULL,
    entity JSONB NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS providers (
    id VARCHAR(36) NOT NULL,
    resource JSONB NOT NULL,
    entity JSONB NOT NULL,
    PRIMARY KEY (id)
);
`
