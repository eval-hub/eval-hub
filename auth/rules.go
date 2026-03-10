package auth

import (
	"bytes"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"text/template"

	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

func matchEndpoint(endpoint string, endpointPattern Endpoint) bool {
	pattern_parts := endpointPattern.PathParts
	if len(pattern_parts) == 0 {
		return false
	}
	endpoint_parts := strings.Split(endpoint, "/")

	for i, part := range pattern_parts {
		if part == "*" {
			continue
		}

		if i >= len(endpoint_parts) || endpoint_parts[i] != part {
			return false
		}
	}

	return true
}

func matchMethods(fromRequest string, fromConfig []string) bool {
	if len(fromConfig) == 0 {
		return true
	}
	m := strings.ToLower(fromRequest)
	return slices.Contains(fromConfig, m)
}

func FindRules(request *http.Request, config *AuthConfig) []ResourceRule {
	for _, endpoint := range config.Authorization.Endpoints {
		if matchEndpoint(request.URL.Path, endpoint) {
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

func AttributesFromRequest(request *http.Request, config *AuthConfig, user user.Info) ([]authorizer.Attributes, error) {
	extractedRules := FindRules(request, config)
	resourceAttributes := []authorizer.Attributes{}

	for _, rule := range extractedRules {
		templateValues := TemplateValues{}
		if rule.Rewrites.ByHttpHeader != nil {
			value := request.Header.Get(rule.Rewrites.ByHttpHeader.Name)
			if value == "" {
				return nil, fmt.Errorf("required header %s is missing", rule.Rewrites.ByHttpHeader.Name)
			}
			templateValues.FromHeader = value
		}
		if rule.Rewrites.ByQueryString != nil {
			value, ok := request.URL.Query()[rule.Rewrites.ByQueryString.Name]
			if !ok || len(value) == 0 {
				return nil, fmt.Errorf("required query string %s is missing", rule.Rewrites.ByQueryString.Name)
			}

			templateValues.FromQueryString = value[0]
		}
		templateValues.FromMethod = httpToKubeVerb(request.Method)

		resourceAttributes = append(resourceAttributes, authorizer.AttributesRecord{
			Namespace:       applyTemplate(rule.ResourceAttributes.Namespace, templateValues),
			APIGroup:        applyTemplate(rule.ResourceAttributes.APIGroup, templateValues),
			APIVersion:      applyTemplate(rule.ResourceAttributes.APIVersion, templateValues),
			Resource:        applyTemplate(rule.ResourceAttributes.Resource, templateValues),
			Subresource:     applyTemplate(rule.ResourceAttributes.Subresource, templateValues),
			Name:            applyTemplate(rule.ResourceAttributes.Name, templateValues),
			Verb:            applyTemplate(rule.ResourceAttributes.Verb, templateValues),
			User:            user,
			ResourceRequest: true,
		})
	}

	return resourceAttributes, nil
}
