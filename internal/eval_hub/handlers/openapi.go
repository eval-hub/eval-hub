package handlers

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/http_wrappers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
)

var (
	noCacheHeaders = map[string]string{
		"Cache-Control": "no-cache, no-store, must-revalidate",
		"Pragma":        "no-cache",
		"Expires":       "0",
	}
)

func (h *Handlers) HandleOpenAPI(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	// Determine content type based on Accept header
	file := "openapi.yaml"
	contentType := "application/yaml"

	if strings.Contains(r.Header("Accept"), "application/json") {
		file = "openapi.json"
		contentType = "application/json"
	}

	// Find the OpenAPI spec file relative to the working directory
	// Try multiple possible locations
	possiblePaths := []string{
		filepath.Join("docs", file),
		filepath.Join("..", "docs", file),
		filepath.Join("..", "..", "docs", file),
		filepath.Join("..", "..", "..", "docs", file),
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
			specPath := filepath.Join(exeDir, "docs", file)
			paths = append(paths, specPath)
			spec, err = os.ReadFile(specPath)
		}
	}

	if err != nil {
		ctx.Logger.Error("Failed to read OpenAPI spec", "paths", paths, "error", err.Error())
		w.ErrorWithMessageCode(ctx.RequestID, messages.InternalServerError, "Error", err.Error())
		return
	}

	w.SetHeader("Content-Type", contentType)
	for key, value := range noCacheHeaders {
		w.SetHeader(key, value)
	}

	w.Write(spec)
}

func (h *Handlers) HandleDocs(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	// Get the base URL for the OpenAPI spec (so without the "/docs" path)
	baseURL := strings.TrimSuffix(r.URI(), r.Path())

	swaggerVersion := "5.32.4"
	if r.Header("SWAGGER_VERSION") != "" {
		swaggerVersion = r.Header("SWAGGER_VERSION")
	}
	swaggerVersion = template.HTMLEscapeString(swaggerVersion)

	html := `<!DOCTYPE html>
<html>
<head>
  <title>Eval Hub API Documentation</title>
  <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@` + swaggerVersion + `/swagger-ui.css" />
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
  <script src="https://unpkg.com/swagger-ui-dist@` + swaggerVersion + `/swagger-ui-bundle.js"></script>
  <script src="https://unpkg.com/swagger-ui-dist@` + swaggerVersion + `/swagger-ui-standalone-preset.js"></script>
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

	for key, value := range noCacheHeaders {
		w.SetHeader(key, value)
	}
	w.SetHeader("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}
