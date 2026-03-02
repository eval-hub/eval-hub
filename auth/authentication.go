package auth

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"k8s.io/apiserver/pkg/apis/apiserver"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authentication/authenticatorfactory"
	"k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/kubernetes"
)

type Authenticator struct {
	auth authenticator.Request

	logger *slog.Logger
}

func NewAuthenticator(client *kubernetes.Clientset, logger *slog.Logger) (*Authenticator, error) {
	if client == nil {
		return nil, fmt.Errorf("client is required")
	}

	tokenClient := client.AuthenticationV1()
	if tokenClient == nil {
		return nil, fmt.Errorf("failed to create authentication client")
	}

	authenticatorConfig := authenticatorfactory.DelegatingAuthenticatorConfig{
		Anonymous: &apiserver.AnonymousAuthConfig{
			Enabled: false,
		},
		CacheTTL:                2 * time.Minute,
		TokenAccessReviewClient: tokenClient,
		WebhookRetryBackoff:     options.DefaultAuthWebhookRetryBackoff(),
	}

	authenticator, _, err := authenticatorConfig.New()

	if err != nil {
		return nil, err
	}

	return &Authenticator{
		auth:   authenticator,
		logger: logger,
	}, nil
}

func (a *Authenticator) AuthenticateRequest(request *http.Request) (*authenticator.Response, bool, error) {
	return a.auth.AuthenticateRequest(request)
}
