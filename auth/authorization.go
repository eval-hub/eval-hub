package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/authorization/authorizerfactory"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/kubernetes"
)

type SarAuthorizer struct {
	auth   authorizer.Authorizer
	config AuthConfig
	client *kubernetes.Clientset
	logger *slog.Logger
}

type AuthorizationError struct {
	Message string
}

func (e *AuthorizationError) Error() string {
	return e.Message
}

func NewSarAuthorizer(client *kubernetes.Clientset, logger *slog.Logger, config *AuthConfig) (*SarAuthorizer, error) {

	if config == nil {
		return nil, fmt.Errorf("config is required")
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
		config: *config,
		client: client,
		logger: logger,
	}, nil
}

func (s *SarAuthorizer) Authorize(ctx context.Context, attributesRecords []authorizer.Attributes) (authorized authorizer.Decision, reason string, err error) {
	for _, record := range attributesRecords {
		s.logger.Info("Authorizing", "record", record)
		decision, reason, err := s.auth.Authorize(ctx, record)
		if err != nil || decision != authorizer.DecisionAllow {
			return decision, reason, err
		}
	}

	return authorizer.DecisionAllow, "", nil
}

func (s *SarAuthorizer) AuthorizeRequest(ctx context.Context, req *http.Request) (authorized authorizer.Decision, reason string, err error) {
	user, ok := request.UserFrom(req.Context())
	if !ok {
		return authorizer.DecisionDeny, "User not found in request context. Please authenticate.", nil
	}
	attributesRecords := AttributesFromRequest(req, s.config, user)
	return s.Authorize(ctx, attributesRecords)
}
