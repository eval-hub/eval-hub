package rbac

import (
	"context"
	"net/http"
	"time"

	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/authorization/authorizerfactory"
	"k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/kubernetes"
)

type SarAuthorizer struct {
	auth   authorizer.Authorizer
	config AuthorizationConfig
	client *kubernetes.Clientset
}

func NewSarAuthorizer(client *kubernetes.Clientset, authConfigPath string) (*SarAuthorizer, error) {
	cfg, err := loadAuthorizerConfig(authConfigPath)
	if err != nil {
		return nil, err
	}

	authorizerConfig := authorizerfactory.DelegatingAuthorizerConfig{
		SubjectAccessReviewClient: client.AuthorizationV1(),
		AllowCacheTTL:             5 * time.Minute,
		DenyCacheTTL:              30 * time.Second,
		WebhookRetryBackoff:       options.DefaultAuthWebhookRetryBackoff(),
	}

	auth, err := authorizerConfig.New()
	if err != nil {
		return nil, err
	}

	return &SarAuthorizer{
		auth:   auth,
		config: *cfg,
		client: client,
	}, nil
}

func (s *SarAuthorizer) authorize(ctx context.Context, auth authorizer.Authorizer, attributesRecords []authorizer.AttributesRecord) bool {
	for _, record := range attributesRecords {

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

func (s *SarAuthorizer) AuthorizeRequest(ctx context.Context, request http.Request) bool {
	attributesRecords := computeResourceAttributeRecords(request, s.config)
	return s.authorize(ctx, s.auth, attributesRecords)
}
