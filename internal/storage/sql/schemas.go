package sql

const SQLITE_SCHEMA = `
CREATE TABLE IF NOT EXISTS evaluations (
    id INTEGER PRIMARY KEY,
    entity TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS collections (
    id INTEGER PRIMARY KEY,
    entity TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_eval_entity
ON evaluations (id);

CREATE INDEX IF NOT EXISTS idx_collection_entity
ON collections (id);
`

const POSTGRES_SCHEMA = `
CREATE TABLE IF NOT EXISTS evaluations (
    id SERIAL PRIMARY KEY,
    entity JSONB NOT NULL
);

CREATE TABLE IF NOT EXISTS collections (
    id SERIAL PRIMARY KEY,
    entity JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_eval_entity
ON evaluations (id);

CREATE INDEX IF NOT EXISTS idx_collection_entity
ON collections (id);
`

func SchemaForDriver(driver string) string {
	switch driver {
	case "sqlite":
		return SQLITE_SCHEMA
	case "postgres":
		return POSTGRES_SCHEMA
	default:
		return ""
	}
}
