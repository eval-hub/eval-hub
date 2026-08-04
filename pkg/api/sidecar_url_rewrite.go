package api

import (
	"log/slog"
	"net/url"
	"strings"
)

// SidecarURLTargets holds real upstream base URLs used to rewrite sidecar localhost
// URLs in persisted status messages. Empty fields mean that target cannot be resolved
// (fallback: strip scheme+host, keep path/query/fragment).
type SidecarURLTargets struct {
	EvalHub       string
	MLFlow        string
	OCI           string
	OCIRepository string
	Model         string
}

// RewriteSidecarURLsInMessages rewrites sidecar base URLs in error and warning messages
// so persisted text shows the real upstream scheme+host instead of localhost.
// When a target cannot be resolved, scheme+host are stripped and the path is kept.
func (e *BenchmarkStatusEvent) RewriteSidecarURLsInMessages(sidecarBaseURL string, targets SidecarURLTargets, logger *slog.Logger) {
	if e == nil {
		return
	}
	rewriteMessageInfo(e.ErrorMessage, sidecarBaseURL, targets, logger)
	rewriteMessageInfo(e.WarningMessage, sidecarBaseURL, targets, logger)
}

func rewriteMessageInfo(m *MessageInfo, sidecarBaseURL string, targets SidecarURLTargets, logger *slog.Logger) {
	if m == nil || m.Message == "" {
		return
	}
	originalMessage := m.Message
	persistedMessage := RewriteSidecarURLsInMessage(originalMessage, sidecarBaseURL, targets)
	if persistedMessage == originalMessage {
		return
	}
	if logger != nil {
		logger.Info("Rewrote sidecar URLs in status message for persistence",
			"original_message", originalMessage,
			"persisted_message", persistedMessage,
		)
	}
	m.Message = persistedMessage
}

// RewriteSidecarURLsInMessage replaces scheme+host of sidecarBaseURL occurrences with
// the real target resolved from the URL path (eval-hub, MLflow, OCI, or model).
// Only URLs whose host exactly matches the sidecar base URL host are rewritten;
// text and URLs that merely share a host prefix are left unchanged.
// When no target is available, scheme+host are removed and path/query/fragment are kept.
func RewriteSidecarURLsInMessage(message, sidecarBaseURL string, targets SidecarURLTargets) string {
	base := strings.TrimRight(strings.TrimSpace(sidecarBaseURL), "/")
	if message == "" || base == "" || !strings.Contains(message, base) {
		return message
	}
	sidecarURL, err := url.Parse(base)
	if err != nil || sidecarURL.Host == "" {
		return message
	}
	sidecarHost := sidecarURL.Host

	var b strings.Builder
	remaining := message
	for {
		i := strings.Index(remaining, base)
		if i < 0 {
			b.WriteString(remaining)
			break
		}
		b.WriteString(remaining[:i])
		rest := remaining[i:]
		end := strings.IndexAny(rest, " \t\n\r")
		var urlStr string
		if end < 0 {
			urlStr = rest
			remaining = ""
		} else {
			urlStr = rest[:end]
			remaining = rest[end:]
		}
		b.WriteString(rewriteSidecarURL(urlStr, sidecarHost, targets))
	}
	return b.String()
}

func rewriteSidecarURL(urlStr, sidecarHost string, targets SidecarURLTargets) string {
	u, err := url.Parse(urlStr)
	if err != nil || u.Host == "" {
		return urlStr
	}
	if u.Host != sidecarHost {
		return urlStr
	}
	path := u.EscapedPath()
	targetBase, ok := resolveSidecarURLTarget(path, targets)
	if !ok {
		return stripURLHost(u)
	}
	tu, err := url.Parse(targetBase)
	if err != nil || tu.Host == "" {
		return stripURLHost(u)
	}
	u.Scheme = tu.Scheme
	u.Host = tu.Host
	u.User = nil
	return u.String()
}

func stripURLHost(u *url.URL) string {
	out := u.EscapedPath()
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		out += "#" + u.Fragment
	}
	if out == "" {
		return "/"
	}
	return out
}

func resolveSidecarURLTarget(path string, targets SidecarURLTargets) (string, bool) {
	switch {
	case strings.HasPrefix(path, "/api/v1/evaluations/"):
		if targets.EvalHub == "" {
			return "", false
		}
		return targets.EvalHub, true
	case isMLflowProxyPath(path):
		if targets.MLFlow == "" {
			return "", false
		}
		return targets.MLFlow, true
	case ociPathMatchesRepository(path, targets.OCIRepository):
		if targets.OCI == "" {
			return "", false
		}
		return targets.OCI, true
	default:
		if targets.Model == "" {
			return "", false
		}
		return targets.Model, true
	}
}

// isMLflowProxyPath mirrors sidecar routing for MLflow REST roots.
func isMLflowProxyPath(path string) bool {
	const (
		mlflowAPIv2PathPrefix          = "/api/2.0/mlflow"
		mlflowAPIv3PathPrefix          = "/api/3.0/mlflow"
		mlflowAPIv2ArtifactsPathPrefix = "/api/2.0/mlflow-artifacts"
	)
	return mlflowPathMatchesPrefix(path, mlflowAPIv2PathPrefix) ||
		mlflowPathMatchesPrefix(path, mlflowAPIv3PathPrefix) ||
		mlflowPathMatchesPrefix(path, mlflowAPIv2ArtifactsPathPrefix)
}

func mlflowPathMatchesPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// ociPathMatchesRepository mirrors sidecar ociRouteMatch: repository segments must appear
// at the start of the path or immediately after a "v2" segment.
func ociPathMatchesRepository(path, repository string) bool {
	repoParts := splitPathSegments(repository)
	if len(repoParts) == 0 {
		return false
	}
	pathParts := splitPathSegments(path)
	if len(pathParts) < len(repoParts) {
		return false
	}
	n := len(repoParts)
	for i := 0; i+n <= len(pathParts); i++ {
		if !pathSegmentsEqual(pathParts[i:i+n], repoParts) {
			continue
		}
		if i == 0 || pathParts[i-1] == "v2" {
			return true
		}
	}
	return false
}

func splitPathSegments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func pathSegmentsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
