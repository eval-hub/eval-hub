package rbac

import (
	"context"
	"time"

	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/authorization/authorizerfactory"
	"k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/kubernetes"
)

func NewSarAuthorizer(client *kubernetes.Clientset) (authorizer.Authorizer, error) {
	authorizerConfig := authorizerfactory.DelegatingAuthorizerConfig{
		SubjectAccessReviewClient: client.AuthorizationV1(),
		AllowCacheTTL:             5 * time.Minute,
		DenyCacheTTL:              30 * time.Second,
		WebhookRetryBackoff:       options.DefaultAuthWebhookRetryBackoff(),
	}
	return authorizerConfig.New()
}

func Authorize(ctx context.Context, auth authorizer.Authorizer, resources []ResourceAttributes) bool {
	for _, resource := range resources {
		record := authorizer.AttributesRecord{
			User:            nil,
			Namespace:       resource.Namespace,
			APIGroup:        resource.APIGroup,
			APIVersion:      resource.APIVersion,
			Resource:        resource.Resource,
			Subresource:     resource.Subresource,
			Verb:            resource.Verb,
			Name:            resource.Name,
			ResourceRequest: true,
		}

		decision, _, err := auth.Authorize(ctx, record)
		if err != nil {
			return false
		}
		if decision != authorizer.DecisionAllow {
			return false
		}
	}

	return true
}
