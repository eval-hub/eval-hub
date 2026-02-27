package sql

// The code in this file must be unware of the database implementation.

import (
	"context"
	db "database/sql"
	"encoding/json"

	"github.com/eval-hub/eval-hub/pkg/api"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
)

type SQLExecutor struct {
	Db  *db.DB
	Txn *db.Tx
	Ctx context.Context
}

func (s *SQLExecutor) Exec(query string, args ...any) (db.Result, error) {
	if s.Txn != nil {
		return s.Txn.ExecContext(s.Ctx, query, args...)
	} else {
		return s.Db.ExecContext(s.Ctx, query, args...)
	}
}

func (s *SQLExecutor) Query(query string, args ...any) (*db.Rows, error) {
	if s.Txn != nil {
		return s.Txn.QueryContext(s.Ctx, query, args...)
	} else {
		return s.Db.QueryContext(s.Ctx, query, args...)
	}
}

func (s *SQLExecutor) QueryRow(query string, args ...any) *db.Row {
	if s.Txn != nil {
		return s.Txn.QueryRowContext(s.Ctx, query, args...)
	} else {
		return s.Db.QueryRowContext(s.Ctx, query, args...)
	}
}

func ApplyPatches(s string, patches *api.Patch) ([]byte, error) {
	if patches == nil || len(*patches) == 0 {
		return []byte(s), nil
	}
	patchesJSON, err := json.Marshal(patches)
	if err != nil {
		return nil, err
	}
	patch, err := jsonpatch.DecodePatch(patchesJSON)
	if err != nil {
		return nil, err
	}
	return patch.Apply([]byte(s))
}
