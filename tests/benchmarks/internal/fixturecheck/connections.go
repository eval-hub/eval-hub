// Package fixturecheck shares model-auth checks between fixture and generator tests.
package fixturecheck

import (
	"encoding/json"
	"sort"
	"strings"
)

// Providers lists the adapters whose smoke inputs use Secret-backed connections.
func Providers() []string {
	// Promptfoo is contrib-only: this repository ships no catalog or fixtures for it.
	return []string{"inspect", "deepeval", "ragas", "followbench", "tooleval", "ibm-clear", "atif"}
}

// IsConnectionOption recognizes connection keys independent of a role prefix.
func IsConnectionOption(key string) bool {
	key = strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	// ToolEval's tool service is not a model connection.
	if key == "toolserverurl" {
		return false
	}
	return strings.HasSuffix(key, "apikey") || strings.HasSuffix(key, "apibase") || strings.HasSuffix(key, "url")
}

// ConnectionOptions returns paths to connection settings, including nested
// framework options and Inspect's legacy JSON-encoded model role specifications.
func ConnectionOptions(value any) []string {
	var found []string
	var walk func(any, string)
	walk = func(value any, path string) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				childPath := key
				if path != "" {
					childPath = path + "." + key
				}
				if IsConnectionOption(key) {
					found = append(found, childPath)
				}
				walk(child, childPath)
			}
		case []any:
			for _, child := range value {
				walk(child, path+"[]")
			}
		case string:
			// Model role strings can hide api_key/base_url in serialized JSON.
			// Other strings are dataset or prompt content, not configuration.
			if strings.HasPrefix(path, "model_roles.") {
				var role any
				if json.Unmarshal([]byte(value), &role) == nil {
					walk(role, path)
				}
			}
		}
	}
	walk(value, "")
	sort.Strings(found)
	return found
}
