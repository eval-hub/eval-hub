package evalcards

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
)

// OCIExportError carries a message suitable for both API responses and logs.
// Untrusted registry bodies, token URLs, and Secret contents are never included.
type OCIExportError struct {
	Code      string
	Operation string
	Cause     string
}

func (e *OCIExportError) Error() string { return e.Operation + ": " + e.Cause }

var registryStatusPattern = regexp.MustCompile(`(?:failed with status|status) ([1-5][0-9]{2})`)

func safeOCICause(err error) string {
	var exportErr *OCIExportError
	if errors.As(err, &exportErr) {
		return exportErr.Error()
	}
	if errors.Is(err, context.Canceled) {
		return "operation was cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "operation timed out"
	}
	if k8serrors.IsNotFound(err) {
		return "tenant connection Secret was not found"
	}
	if k8serrors.IsForbidden(err) {
		return "access to the tenant connection Secret was forbidden"
	}
	if k8serrors.IsUnauthorized(err) {
		return "tenant connection Secret access was unauthorized"
	}
	var unsupportedType *json.UnsupportedTypeError
	if errors.As(err, &unsupportedType) {
		return "card contains an unsupported JSON value type"
	}
	var unsupportedValue *json.UnsupportedValueError
	if errors.As(err, &unsupportedValue) {
		return "card contains a non-finite or cyclic JSON value"
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return "registry credentials or response contain invalid JSON"
	}
	var cert x509.UnknownAuthorityError
	if errors.As(err, &cert) {
		return "registry TLS certificate authority is not trusted"
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "registry request timed out"
		}
		return "registry connection failed"
	}
	if match := registryStatusPattern.FindStringSubmatch(err.Error()); len(match) == 2 {
		return fmt.Sprintf("registry request failed with HTTP status %s", match[1])
	}
	// Only fixed, service-defined causes are copied. An arbitrary upstream error
	// may echo credentials, so it must not be used as the API or log message.
	for _, cause := range []string{
		"oci secret getter is not configured", "oci k8s connection secret is required",
		"tenant namespace is required", "oci credential resolver is not configured",
		"oci http client is not configured", "registry host is required", "repository is required",
		"no auths", "no auth for registry", "missing username/password", "decode auth",
		"auth response missing token", "no WWW-Authenticate header", "not a Bearer challenge",
		"no realm in challenge", "insecure token realm", "unsupported token realm scheme",
		"discard publisher configured", "oci publisher is not configured", "unauthorized",
		"parse oci ca cert", "read oci ca cert", "start blob upload missing Location header",
	} {
		if strings.Contains(err.Error(), cause) {
			return cause
		}
	}
	return "publisher, credentials, or registry access is unavailable"
}
