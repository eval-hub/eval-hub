package api

import (
	"net"
	"strings"
	"testing"
)

func TestValidateGitCloneURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{name: "https github", url: "https://github.com/org/repo.git"},
		{name: "http public", url: "http://git.example.com/repo.git"},
		{name: "empty", url: "", wantErr: "required"},
		{name: "ssh rejected by scheme", url: "ssh://git@github.com/org/repo.git", wantErr: "http or https"},
		{name: "loopback ipv4", url: "https://127.0.0.1/repo.git", wantErr: "not allowed"},
		{name: "loopback ipv6", url: "https://[::1]/repo.git", wantErr: "not allowed"},
		{name: "private 10", url: "https://10.0.0.5/repo.git", wantErr: "not allowed"},
		{name: "private 192", url: "https://192.168.1.10/repo.git", wantErr: "not allowed"},
		{name: "link local", url: "https://169.254.169.254/latest", wantErr: "not allowed"},
		{name: "localhost", url: "https://localhost/repo.git", wantErr: "not allowed"},
		{name: "cluster local", url: "https://evalhub.ns.svc.cluster.local:8443/repo.git", wantErr: "not allowed"},
		{name: "svc suffix", url: "https://myservice.ns.svc/repo.git", wantErr: "not allowed"},
		{name: "mdns local", url: "https://git.local/repo.git", wantErr: "not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateGitCloneURL(tt.url)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateGitCloneURL(%q) = %v, want nil", tt.url, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateGitCloneURL(%q) = nil, want error containing %q", tt.url, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateGitCloneURL(%q) = %v, want substring %q", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestValidateGitCloneURLResolved(t *testing.T) {
	t.Parallel()
	lookup := func(host string) ([]net.IP, error) {
		switch host {
		case "evil.example.com":
			return []net.IP{net.ParseIP("10.1.2.3")}, nil
		case "ok.example.com":
			return []net.IP{net.ParseIP("1.2.3.4")}, nil
		default:
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
	}
	if err := ValidateGitCloneURLResolved("https://ok.example.com/repo.git", lookup); err != nil {
		t.Fatalf("expected ok host to pass, got %v", err)
	}
	err := ValidateGitCloneURLResolved("https://evil.example.com/repo.git", lookup)
	if err == nil || !strings.Contains(err.Error(), "disallowed address") {
		t.Fatalf("expected disallowed address error, got %v", err)
	}
}
