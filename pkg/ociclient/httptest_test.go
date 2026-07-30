package ociclient

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// startTestServer starts an httptest server and returns a client whose connections
// abort with SO_LINGER=0 (RST) instead of lingering in TIME_WAIT. That avoids
// ephemeral-port exhaustion when many registry mock servers run under -count /
// parallel package tests.
func startTestServer(t *testing.T, handler http.Handler) (*httptest.Server, *http.Client) {
	t.Helper()

	srv := httptest.NewServer(handler)
	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				var d net.Dialer
				c, err := d.DialContext(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				if tc, ok := c.(*net.TCPConn); ok {
					_ = tc.SetLinger(0)
				}
				return c, nil
			},
		},
	}
	t.Cleanup(func() {
		client.CloseIdleConnections()
		srv.Close()
	})
	return srv, client
}
