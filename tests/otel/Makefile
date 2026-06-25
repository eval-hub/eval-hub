.PHONY: start-signoz stop-signoz logs-signoz bootstrap-pours forge-signoz start-otel-collector stop-otel-collector

PODMAN ?= podman
COMPOSE_FILE ?= pours/deployment/compose.yaml
FOUNDRYCTL ?= foundryctl

start-signoz: ## Start local SigNoz stack (Podman Compose)
	$(PODMAN) compose -f $(COMPOSE_FILE) up -d

stop-signoz: ## Stop local SigNoz stack
	$(PODMAN) compose -f $(COMPOSE_FILE) down

logs-signoz: ## Follow SigNoz ingester and UI logs
	$(PODMAN) compose -f $(COMPOSE_FILE) logs -f ingester signoz-signoz-0

bootstrap-pours: ## Refresh pours/deployment from SigNoz Foundry example
	./scripts/bootstrap-pours.sh

forge-signoz: ## Regenerate pours/deployment with foundryctl (requires casting.yaml)
	$(FOUNDRYCTL) forge -f casting.yaml -p pours
	@# Keep SigNoz UI off eval-hub's default :8080.
	@python3 -c "from pathlib import Path; p=Path('pours/deployment/compose.yaml'); t=p.read_text(); p.write_text(t.replace('8080:8080','3301:8080')) if '8080:8080' in t else None"

# Backward-compatible aliases
start-otel-collector: start-signoz
stop-otel-collector: stop-signoz
