package server

import (
	"context"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

func TestNewOCIPublisherFactoryLocalModeReturnsNoop(t *testing.T) {
	t.Parallel()

	factory := newOCIPublisherFactory(nil, &config.Config{
		Service: &config.ServiceConfig{LocalMode: true},
	})
	publisher, err := factory.NewPublisher(context.Background(), nil)
	if err != nil {
		t.Fatalf("NewPublisher() err = %v", err)
	}
	if err := publisher.PublishEvalCard(context.Background(), []byte(`{"card_version":"1.0"}`)); err != nil {
		t.Fatalf("PublishEvalCard() err = %v", err)
	}
	if err := publisher.Close(); err != nil {
		t.Fatalf("Close() err = %v", err)
	}
}

func TestNewOCIPublisherFactoryNilConfigReturnsNoop(t *testing.T) {
	t.Parallel()

	factory := newOCIPublisherFactory(nil, nil)
	publisher, err := factory.NewPublisher(context.Background(), nil)
	if err != nil {
		t.Fatalf("NewPublisher() err = %v", err)
	}
	if err := publisher.PublishEvalCard(context.Background(), []byte(`{}`)); err != nil {
		t.Fatalf("PublishEvalCard() err = %v", err)
	}
}
