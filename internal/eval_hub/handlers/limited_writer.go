package handlers

import (
	"errors"
	"io"
)

// ErrLogResponseTruncated is returned by LimitedWriter when the configured byte limit is reached.
var ErrLogResponseTruncated = errors.New("log response truncated: size limit exceeded")

// LimitedWriter wraps an io.Writer and stops writing after limit bytes have been written.
// When the limit is exceeded, subsequent writes return ErrLogResponseTruncated.
// A limit of -1 means unlimited (no cap).
type LimitedWriter struct {
	W       io.Writer
	Limit   int64
	written int64
}

func (lw *LimitedWriter) Write(p []byte) (int, error) {
	if lw.Limit == -1 {
		return lw.W.Write(p)
	}
	if lw.written >= lw.Limit {
		return 0, ErrLogResponseTruncated
	}
	allowed := lw.Limit - lw.written
	if int64(len(p)) > allowed {
		n, err := lw.W.Write(p[:allowed])
		lw.written += int64(n)
		if err != nil {
			return n, err
		}
		return n, ErrLogResponseTruncated
	}
	n, err := lw.W.Write(p)
	lw.written += int64(n)
	return n, err
}
