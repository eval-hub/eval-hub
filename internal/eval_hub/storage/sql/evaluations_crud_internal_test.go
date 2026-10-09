package sql

import (
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	se "github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
)

type stubEvaluationJobDeleteResult struct {
	rows int64
	err  error
}

func (r stubEvaluationJobDeleteResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (r stubEvaluationJobDeleteResult) RowsAffected() (int64, error) {
	return r.rows, r.err
}

func TestEvaluationJobDeleteResultError(t *testing.T) {
	store := &sqlStorage{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	tests := []struct {
		name     string
		result   sql.Result
		wantErr  bool
		wantCode *messages.MessageCode
	}{
		{
			name:     "rows affected error",
			result:   stubEvaluationJobDeleteResult{err: errors.New("rows affected unavailable")},
			wantErr:  true,
			wantCode: messages.DatabaseOperationFailed,
		},
		{
			name:     "zero rows",
			result:   stubEvaluationJobDeleteResult{},
			wantErr:  true,
			wantCode: messages.ResourceNotFound,
		},
		{
			name:   "deleted row",
			result: stubEvaluationJobDeleteResult{rows: 1},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := store.evaluationJobDeleteResultError("job-1", test.result)
			if !test.wantErr {
				if err != nil {
					t.Fatalf("evaluationJobDeleteResultError() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("evaluationJobDeleteResultError() error = nil, want an error")
			}
			var serviceErr *se.ServiceError
			if !errors.As(err, &serviceErr) {
				t.Fatalf("evaluationJobDeleteResultError() error = %T, want *ServiceError", err)
			}
			if got := serviceErr.MessageCode(); got != test.wantCode {
				t.Fatalf("error message code = %q, want %q", got.GetCode(), test.wantCode.GetCode())
			}
		})
	}
}
