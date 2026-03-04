package main

import (
	"fmt"
	"path"
	"strings"
)

func matchPath(endpoint string, pattern string) bool {
	if idx := strings.Index(pattern, "*"); idx != -1 {
		suffix := pattern[idx:]
		endpoint_suffix := endpoint[idx:]
		match, err := path.Match(suffix, endpoint_suffix)
		if err != nil {
			return false
		}
		return match
	}
	return strings.HasPrefix(endpoint, pattern)
}

func main() {
	endpoint := "/api/v1/evaluations/jobs/234234234234/events"

	pattern := "/api/v1/evaluations/jobs/*"

	fmt.Println(matchPath(endpoint, pattern))
}
