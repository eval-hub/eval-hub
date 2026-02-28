package auth

import (
	"bytes"
	"net/http"
	"slices"
	"strings"
	"text/template"

	"k8s.io/apiserver/pkg/authorization/authorizer"
)

func matchEndpoint(fromRequest string, fromConfig string) bool {
	return strings.HasPrefix(fromRequest, fromConfig)
}

func matchMethods(fromRequest string, fromConfig []string) bool {
	if len(fromConfig) == 0 {
		return true
	}
	m := strings.ToLower(fromRequest)
	return slices.Contains(fromConfig, m)
}

func extractRule(request *http.Request, config EndpointsAuthorizationConfig) []ResourceRule {
	for _, endpoint := range config.Authorization.Endpoints {
		if matchEndpoint(request.URL.Path, endpoint.Path) {
			for _, mapping := range endpoint.Mappings {
				if matchMethods(request.Method, mapping.Methods) {
					return mapping.Resources
				}
			}
		}
	}
	return nil
}

type TemplateValues struct {
	FromHeader      string
	FromQueryString string
	FromMethod      string
}

func httpToKubeVerb(httpVerb string) string {
	switch httpVerb {
	case "GET":
		return "get"
	case "POST":
		return "create"
	case "PUT":
		return "update"
	case "DELETE":
		return "delete"
	case "PATCH":
		return "patch"
	case "OPTIONS":
		return "options"
	case "HEAD":
		return "head"
	}
	return ""
}

func applyTemplate(templateString string, values TemplateValues) string {
	tmpl, _ := template.New("valueTemplate").Parse(templateString)
	out := bytes.NewBuffer(nil)
	err := tmpl.Execute(out, values)
	if err != nil {
		return ""
	}
	return out.String()
}

func AttributesRecordFromRequest(request *http.Request, config EndpointsAuthorizationConfig) []authorizer.AttributesRecord {
	extractedRules := extractRule(request, config)
	resourceAttributes := []authorizer.AttributesRecord{}

	for _, rule := range extractedRules {
		templateValues := TemplateValues{}
		if rule.Rewrites.ByHttpHeader != nil {
			value, ok := request.Header[rule.Rewrites.ByHttpHeader.Name]
			if ok && len(value) > 0 {
				templateValues.FromHeader = value[0]
			}
		}
		if rule.Rewrites.ByQueryString != nil {
			value, ok := request.URL.Query()[rule.Rewrites.ByQueryString.Name]
			if ok && len(value) > 0 {
				templateValues.FromQueryString = value[0]
			}
		}
		templateValues.FromMethod = httpToKubeVerb(request.Method)

		resourceAttributes = append(resourceAttributes, authorizer.AttributesRecord{
			Namespace:   applyTemplate(rule.ResourceAttributes.Namespace, templateValues),
			APIGroup:    applyTemplate(rule.ResourceAttributes.APIGroup, templateValues),
			APIVersion:  applyTemplate(rule.ResourceAttributes.APIVersion, templateValues),
			Resource:    applyTemplate(rule.ResourceAttributes.Resource, templateValues),
			Subresource: applyTemplate(rule.ResourceAttributes.Subresource, templateValues),
			Name:        applyTemplate(rule.ResourceAttributes.Name, templateValues),
			Verb:        applyTemplate(rule.ResourceAttributes.Verb, templateValues),
		})
	}
	return resourceAttributes
}
