package server

import "net/http"

const remoteUserHeader = "Remote-User"

func remoteUserFromRequest(r *http.Request, rbacProxyAuth bool) string {
	remoteUser := r.Header.Get(USER_HEADER)
	if rbacProxyAuth {
		return remoteUser
	}
	if remoteUser == "" && r.URL != nil && r.URL.User != nil {
		remoteUser = r.URL.User.Username()
	}
	if remoteUser == "" {
		remoteUser = r.Header.Get(remoteUserHeader)
	}
	return remoteUser
}
