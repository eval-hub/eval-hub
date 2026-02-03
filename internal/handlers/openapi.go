package handlers

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/messages"
)

func (h *Handlers) HandleOpenAPI(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {

	// Determine content type based on Accept header
	accept := r.Header("Accept")
	contentType := "application/yaml"
	if strings.Contains(accept, "application/json") {
		contentType = "application/json"
	}

	w.SetHeader("Content-Type", contentType)

	// Find the OpenAPI spec file relative to the working directory
	// Try multiple possible locations
	possiblePaths := []string{
		filepath.Join("docs", "openapi.yaml"),
		filepath.Join("..", "docs", "openapi.yaml"),
		filepath.Join("..", "..", "docs", "openapi.yaml"),
		filepath.Join("..", "..", "..", "docs", "openapi.yaml"),
	}

	var paths []string
	var spec []byte
	var err error
	for _, path := range possiblePaths {
		absPath, aerr := filepath.Abs(path)
		if aerr != nil {
			ctx.Logger.Error("Failed to get absolute path for OpenAPI spec", "path", path, "error", aerr.Error())
			continue
		}
		paths = append(paths, absPath)
		spec, err = os.ReadFile(absPath)
		if err == nil {
			break
		}
	}

	if err != nil {
		// If file not found, try to find it relative to the executable
		exePath, _ := os.Executable()
		if exePath != "" {
			exeDir := filepath.Dir(exePath)
			specPath := filepath.Join(exeDir, "docs", "openapi.yaml")
			paths = append(paths, specPath)
			spec, err = os.ReadFile(specPath)
		}
	}

	if err != nil {
		ctx.Logger.Error("Failed to read OpenAPI spec", "paths", paths, "error", err.Error())
		w.ErrorWithMessageCode(ctx.RequestID, messages.InternalServerError, "Error", err.Error())
		return
	}

	w.Write(spec)
}

func (h *Handlers) HandleDocs(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {

	// Get the base URL for the OpenAPI spec
	baseURL := r.URI()

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

	w.SetHeader("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}
