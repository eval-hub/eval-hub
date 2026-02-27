package rbac

import (
	"bytes"
	"slices"
	"strings"
	"text/template"
)

type Headers map[string][]string
type QueryStrings map[string][]string

type FromRequest struct {
	Endpoint     string
	Headers      Headers
	QueryStrings QueryStrings
	Method       string
}

func matchEndpoint(fromRequest string, fromConfig string) bool {
	return strings.HasPrefix(fromRequest, fromConfig)
}

func matchMethods(fromRequest string, fromConfig []string) bool {

	if fromConfig == nil || len(fromConfig) == 0 {
		return true
	}
	m := strings.ToLower(fromRequest)
	return slices.Contains(fromConfig, m)
}

func extractRule(request FromRequest, config AuthorizationConfig) []ResourceRule {
	for _, endpoint := range config.Authorization.Endpoints {
		if matchEndpoint(request.Endpoint, endpoint.Path) {
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

func ComputeResourceAttributes(request FromRequest, config AuthorizationConfig) []ResourceAttributes {
	extractedRules := extractRule(request, config)
	resourceAttributes := []ResourceAttributes{}

	for _, rule := range extractedRules {
		templateValues := TemplateValues{}
		if rule.Rewrites.ByHttpHeader != nil {
			value, ok := request.Headers[rule.Rewrites.ByHttpHeader.Name]
			if ok && len(value) > 0 {
				templateValues.FromHeader = value[0]
			}
		}
		if rule.Rewrites.ByQueryString != nil {
			value, ok := request.QueryStrings[rule.Rewrites.ByQueryString.Name]
			if ok && len(value) > 0 {
				templateValues.FromQueryString = value[0]
			}
		}
		templateValues.FromMethod = httpToKubeVerb(request.Method)

		resourceAttributes = append(resourceAttributes, ResourceAttributes{
			Namespace: applyTemplate(rule.ResourceAttributes.Namespace, templateValues),
			APIGroup:  applyTemplate(rule.ResourceAttributes.APIGroup, templateValues),
			Resource:  applyTemplate(rule.ResourceAttributes.Resource, templateValues),
			Verb:      applyTemplate(rule.ResourceAttributes.Verb, templateValues),
		})
	}
	return resourceAttributes
}
