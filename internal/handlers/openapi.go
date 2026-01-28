package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
)

func (h *Handlers) HandleOpenAPI(ctx *executioncontext.ExecutionContext, w http.ResponseWriter) {
	if ctx.Request.Method() != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Determine content type based on Accept header
	accept := ctx.Request.Header("Accept")
	contentType := "application/yaml"
	if strings.Contains(accept, "application/json") {
		contentType = "application/json"
	}

	w.Header().Set("Content-Type", contentType)

	// Find the OpenAPI spec file relative to the working directory
	// Try multiple possible locations
	possiblePaths := []string{
		"api/openapi.yaml",
		"../../api/openapi.yaml",
		filepath.Join("api", "openapi.yaml"),
	}

	var spec []byte
	var err error
	for _, path := range possiblePaths {
		spec, err = os.ReadFile(path)
		if err == nil {
			break
		}
	}

	if err != nil {
		// If file not found, try to find it relative to the executable
		exePath, _ := os.Executable()
		if exePath != "" {
			exeDir := filepath.Dir(exePath)
			specPath := filepath.Join(exeDir, "api", "openapi.yaml")
			spec, err = os.ReadFile(specPath)
		}
	}

	if err != nil {
		http.Error(w, "Failed to read OpenAPI spec: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(spec)
}

func (h *Handlers) HandleDocs(ctx *executioncontext.ExecutionContext, w http.ResponseWriter) {
	if ctx.Request.Method() != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get the base URL for the OpenAPI spec
	baseURL := ctx.Request.URI()

	html := `<!DOCTYPE html>
<html>
<head>
  <title>Eval Hub Backend Service API Documentation</title>
  <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui.css" />
  <style>
    html {
      box-sizing: border-box;
      overflow: -moz-scrollbars-vertical;
      overflow-y: scroll;
    }
    *, *:before, *:after {
      box-sizing: inherit;
    }
    body {
      margin:0;
      background: #fafafa;
    }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui-bundle.js"></script>
  <script src="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui-standalone-preset.js"></script>
  <script>
    window.onload = function() {
      const ui = SwaggerUIBundle({
        url: "` + baseURL + `/openapi.yaml",
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIStandalonePreset
        ],
        plugins: [
          SwaggerUIBundle.plugins.DownloadUrl
        ],
        layout: "StandaloneLayout"
      });
    };
  </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}
