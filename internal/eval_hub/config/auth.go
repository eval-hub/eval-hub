package config

const (
	// AuthTypeRBACProxy indicates eval-hub runs behind kube-rbac-proxy, which sets X-User.
	AuthTypeRBACProxy = "rbac-proxy"
)

// IsRBACProxyAuth reports whether --auth-type rbac-proxy was set at startup.
func (c *ServiceConfig) IsRBACProxyAuth() bool {
	return c != nil && c.AuthType == AuthTypeRBACProxy
}
