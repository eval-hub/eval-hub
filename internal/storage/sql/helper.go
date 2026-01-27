package sql

func createEvaluationsTable() string {
	return `CREATE TABLE IF NOT EXISTS Evaluations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    entity TEXT NOT NULL CHECK (json_valid(entity))
);`
}

func createCollectionsTable() string {
	return `CREATE TABLE IF NOT EXISTS Collections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    entity TEXT NOT NULL CHECK (json_valid(entity))
);`
}

// createAddEntityStatement the order or arguments is:
// table_name entity
func createAddEntityStatement() string {
	return `INSERT INTO ? (entity)
	VALUES (?);`
}
