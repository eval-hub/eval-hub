# Contributing to Eval Hub

Thank you for your interest in contributing to Eval Hub! This document provides guidelines for contributing to the project.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [How to Contribute](#how-to-contribute)
- [Development Workflow](#development-workflow)
- [Code Standards](#code-standards)
- [Testing](#testing)
- [Pull Request Process](#pull-request-process)
- [Issue Reporting](#issue-reporting)
- [Documentation](#documentation)
- [Community](#community)

## Code of Conduct

This project and everyone participating in it is governed by our Code of Conduct. By participating, you are expected to uphold this code. Please report unacceptable behavior to the project maintainers.

## Getting Started

Eval Hub is an API REST server that serves as a routing and orchestration layer for evaluation backends. It supports flexible deployment options from local development to production Kubernetes/OpenShift clusters. Before contributing, familiarize yourself with:

- **Architecture**: Read the [README.md](README.md) for project overview
- **API Documentation**: Check [API.md](API.md) for endpoint specifications
- **Deployment Options**: Understand local development, Podman, and Kubernetes/OpenShift deployment models

### Prerequisites

**Required for All Development:**
- Go 1.25.0+
- [Make](https://www.gnu.org/software/make/) for build automation
- Git

**Optional for Container Testing:**
- Podman (for containerization testing)

**Optional for Cluster Integration Testing:**
- Access to a Kubernetes/OpenShift cluster
- kubectl or oc CLI tools

## Development Setup

1. **Fork and Clone**
   ```bash
   git clone https://github.com/your-username/eval-hub.git
   cd eval-hub
   ```

2. **Install Dependencies**
   ```bash
   # Download and tidy Go dependencies
   make install-deps
   ```

3. **Configure Environment**
   ```bash
   cp .env.example .env
   # Edit .env with your local configuration
   # Or edit config/config.yaml directly
   ```

4. **Install Pre-commit Hooks**
   ```bash
   pre-commit install
   ```

5. **Verify Setup**
   ```bash
   # Run tests to verify everything works
   make test

   # Start the development server (default port 8080)
   make start-service

   # Or use a custom port
   PORT=3000 make start-service
   ```

## How to Contribute

We welcome contributions in various forms:

### Types of Contributions

- **Bug Fixes**: Fix issues in existing functionality
- **Features**: Add new evaluation backends, API endpoints, or capabilities
- **Documentation**: Improve README, API docs, or add examples
- **Testing**: Add test coverage or improve test infrastructure
- **Performance**: Optimize existing code or reduce resource usage
- **DevOps**: Improve CI/CD, deployment, or monitoring

### Contribution Areas

1. **Backend Executors**: Add support for new evaluation frameworks
2. **API Endpoints**: Extend the REST API with new functionality
3. **Deployment Integration**: Improve local, Podman, or Kubernetes deployment and orchestration
4. **MLFlow Integration**: Enhance experiment tracking capabilities
5. **Monitoring**: Add metrics, logging, or health checks
6. **Documentation**: User guides, API documentation, examples

## Development Workflow

### 1. Create an Issue

Before starting work, create an issue to discuss:
- **Bug Reports**: Describe the problem with reproduction steps
- **Feature Requests**: Explain the use case and proposed solution
- **Architectural Changes**: See special requirements below
- **Questions**: Ask for clarification or guidance

#### Architectural Changes

**Definition**: Changes that affect system design, component interactions, or technology choices, including:
- New backend executors or evaluation frameworks
- API endpoint additions or modifications
- Database schema changes
- Deployment architecture updates
- New dependencies or technology stack changes
- Performance or security architectural decisions

**Required Process**:
1. **Create Issue**: Use `kind/architecture` label
2. **Discussion**: Allow community input and maintainer feedback in the issue
3. **Approval**: Maintainers add `status/accepted` label after discussion
4. **Implementation**: Only proceed with implementation after approval
5. **Closure**: Issues without approval will be closed with explanation

**Note**: Implementation PRs for architectural changes will only be accepted if the corresponding issue has `status/accepted` label.

### 2. Branch Strategy

```bash
# Create a feature branch from main
git checkout main
git pull origin main
git checkout -b feature/your-feature-name

# Or for bug fixes
git checkout -b fix/issue-description
```

### 3. Development Process

1. **Write Tests First**: For new features, write tests before implementation
2. **Implement Changes**: Write code following our standards
3. **Test Locally**: Run full test suite and verify functionality
4. **Document Changes**: Update relevant documentation

### 4. Commit Guidelines

Use conventional commits:
```bash
# Format: type(scope): description
git commit -m "feat(api): add collection-based evaluation endpoint"
git commit -m "fix(executor): handle timeout errors in NeMo evaluator"
git commit -m "docs(readme): update deployment instructions"
git commit -m "test(integration): add MLFlow integration tests"
```

**Types**: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `ci`, `chore`

PRs targeting `main` will fail CI if any commit message does not follow this format.

If you have [pre-commit](https://pre-commit.com) installed, commit messages are also checked locally:

```bash
pre-commit install --hook-type commit-msg
```

## Code Standards

### Code Quality Tools

We use automated tools to maintain code quality:

```bash
# Format code
make fmt

# Lint code
make lint

# Vet code
make vet

# Run all quality checks
pre-commit run --all-files
```

### Go Standards

- **Go Version**: Support 1.25.0+
- **Code Style**: Follow standard Go conventions (enforced by gofmt)
- **Error Handling**: Always check and handle errors explicitly
- **Documentation**: Use godoc-style comments for exported types and functions
- **Import Grouping**: Standard library, then external packages, then internal packages

### Code Organization

- **Packages**: Keep packages focused and cohesive
- **Dependencies**: Add new dependencies carefully
- **Error Handling**: Return errors explicitly; use error wrapping with `fmt.Errorf` and `%w`
- **Logging**: Use structured logging with zap (wrapped in slog interface)
- **Configuration**: Use Viper for configuration management

### Example Code Structure

```go
// Package handlers provides HTTP request handlers for evaluation operations.
package handlers

import (
	"encoding/json"

	"github.com/your-org/eval-hub/internal/executioncontext"
)

// EvaluationRequest represents an evaluation request.
type EvaluationRequest struct {
	Model          string   `json:"model"`
	Benchmarks     []string `json:"benchmarks"`
	ExperimentName string   `json:"experiment_name,omitempty"`
}

// HandleCreateEvaluation processes an evaluation request.
// Returns evaluation results or an error.
func (h *Handlers) HandleCreateEvaluation(ctx *executioncontext.ExecutionContext, w http.ResponseWriter, r *http.Request) {
	var req EvaluationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ctx.Logger.Error("Failed to decode request", "error", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	ctx.Logger.Info("Processing evaluation", "model", req.Model)
	// Implementation here
}
```

## Testing

### Test Categories

- **Unit Tests**: Test individual functions and packages (in `internal/`)
- **FVT (Functional Verification Tests)**: BDD-style tests using godog (in `tests/features/`)
- **Integration Tests**: Test component interactions

### Running Tests

```bash
# Run all tests (unit + FVT)
make test-all

# Run only unit tests
make test

# Run only FVT tests
make test-fvt

# Generate FVT HTML report (requires Node dev deps)
npm install
make fvt-report

# Run tests with coverage
make test-coverage

# Run specific unit test
go test -v ./internal/handlers -run TestHandleName

# Run specific FVT test
go test -v ./tests/features -run TestFeatureName
```

### Test Requirements

1. **New Features**: Must include unit and integration tests
2. **Bug Fixes**: Must include regression tests
3. **Coverage**: Maintain >80% test coverage
4. **Performance**: Include performance tests for critical paths

### Test Structure

```go
package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/your-org/eval-hub/internal/executioncontext"
)

func TestHandleCreateEvaluation_Success(t *testing.T) {
	// Arrange
	req := httptest.NewRequest(http.MethodPost, "/api/v1/evaluations/jobs", nil)
	w := httptest.NewRecorder()

	// Act
	handler.HandleCreateEvaluation(ctx, w, req)

	// Assert
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandleCreateEvaluation_Timeout(t *testing.T) {
	// Test timeout handling
}
```

## OpenShift Deployment Testing

EvalHub can be deployed on OpenShift via the [TrustyAI operator](https://github.com/trustyai-explainability/trustyai-service-operator), which is included in [OpenDataHub](https://opendatahub.io/).

### Prerequisites

- Access to an OpenShift cluster
- Cluster admin permissions or sufficient RBAC permissions
- A container registry account (e.g., quay.io) for hosting your custom EvalHub image

### Deployment Steps

1. **Install OpenDataHub from OperatorHub**

   Install OpenDataHub 3.3 (recommended) from the OpenShift OperatorHub:
   - Navigate to Operators → OperatorHub in the OpenShift console
   - Search for "Open Data Hub"
   - Install version 3.3 (or latest stable version)

2. **Create a DataScienceCluster**

   Create a DataScienceCluster with the TrustyAI component enabled (enabled by default):

   ```yaml
   apiVersion: datasciencecluster.opendatahub.io/v1
   kind: DataScienceCluster
   metadata:
     name: default-dsc
   spec:
     components:
       trustyai:
         managementState: Managed
   ```

3. **Build and Push Your EvalHub Image**

   Build your custom EvalHub image and push it to a container registry:

   ```bash
   # Build the image
   podman build -t quay.io/<your-username>/eval-hub:latest .

   # Push to registry
   podman push quay.io/<your-username>/eval-hub:latest
   ```

4. **Update Manifests with Custom Image**

   In your fork of the TrustyAI operator, update the `params.env` file in your manifests to reference your custom EvalHub image:

   ```env
   evalHubImage=quay.io/<your-username>/eval-hub:latest
   ```

5. **Configure Custom Image Reference**

   You have two options to use your custom image:

   **Option A: Using devFlags**

   Update your DataScienceCluster to reference your custom manifests:

   ```yaml
   apiVersion: datasciencecluster.opendatahub.io/v1
   kind: DataScienceCluster
   metadata:
     name: default-dsc
   spec:
     components:
       trustyai:
         devFlags:
           manifests:
             - contextDir: config
               sourcePath: ""
               uri: "https://github.com/<your-org>/trustyai-service-operator/tarball/<your-branch>"
         managementState: Managed
   ```

   **Option B: Mount manifests directly**

   Update the manifest files with your custom image reference and mount them to the operator. See the [OpenDataHub Component Development Guide](https://github.com/opendatahub-io/opendatahub-operator/blob/main/hack/component-dev/README.md) for details on mounting manifests.

6. **Deploy an EvalHub Custom Resource**

   Create an EvalHub CR to deploy your instance:

   ```yaml
   apiVersion: trustyai.opendatahub.io/v1alpha1
   kind: EvalHub
   metadata:
     name: evalhub-instance
     namespace: <your-namespace>
   spec:
     # Add your EvalHub configuration here
   ```

### Additional Resources

For more detailed information on deployment and development workflows:
- [TrustyAI Service Operator](https://github.com/trustyai-explainability/trustyai-service-operator)
- [OpenDataHub Component Development Guide](https://github.com/opendatahub-io/opendatahub-operator/blob/main/hack/component-dev/README.md)
- [OpenDataHub Documentation](https://opendatahub.io/)

## Pull Request Process

### Before Submitting

1. **Rebase on Main**: Ensure your branch is up-to-date
   ```bash
   git checkout main
   git pull origin main
   git checkout your-branch
   git rebase main
   ```

2. **Run Full Test Suite**
   ```bash
   pytest
   pre-commit run --all-files
   ```

3. **Update Documentation**: Include relevant documentation updates

### PR Template

When creating a pull request, include:

**Description**
- Brief summary of changes
- Link to related issue(s)

**Type of Change**
- [ ] Bug fix (non-breaking change)
- [ ] New feature (non-breaking change)
- [ ] Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] Documentation update

**Testing**
- [ ] Unit tests pass
- [ ] Integration tests pass
- [ ] New tests added for new functionality

**Checklist**
- [ ] Code follows project style guidelines
- [ ] Self-review of code completed
- [ ] Documentation updated
- [ ] No new warnings introduced

### Review Process

1. **Automated Checks**: CI must pass (tests, linting, type checking)
2. **OWNERS Assignment**: TBD - Project maintainers will be assigned as reviewers
3. **Code Review**: Component experts and maintainer approval required
4. **Testing**: Reviewers may test functionality manually
5. **Documentation**: Ensure documentation is clear and complete

## Issue Reporting

We use a structured labeling system with `kind/*` prefixes to categorize issues.

### Bug Reports

When reporting bugs, include:

```markdown
**Description**: Clear description of the issue

**To Reproduce**: Steps to reproduce the behavior
1. Go to '...'
2. Click on '....'
3. See error

**Expected Behavior**: What you expected to happen

**Environment**:
- OS: [e.g. Ubuntu 22.04]
- Go Version: [e.g. 1.25.0]
- eval-hub Version: [e.g. 0.1.1]
- Kubernetes Version: [e.g. 1.28]

**Additional Context**: Any additional information
```

### Feature Requests

For feature requests, include:

```markdown
**Problem Statement**: What problem does this solve?

**Proposed Solution**: Describe your proposed solution

**Alternatives**: Any alternative solutions considered

**Use Case**: Real-world scenario where this would be useful

**Implementation Notes**: Technical considerations or constraints
```

## Documentation

### Types of Documentation

1. **API Documentation**: OpenAPI specs and endpoint documentation
2. **User Guides**: How-to guides for common tasks
3. **Developer Docs**: Architecture and implementation details
4. **Deployment Guides**: Kubernetes/OpenShift deployment instructions

### Documentation Standards

- **Clarity**: Write for your intended audience
- **Examples**: Include practical examples
- **Accuracy**: Keep documentation in sync with code
- **Structure**: Use consistent formatting and organization

### Building Documentation

```bash
# The OpenAPI specification is maintained in docs/openapi.yaml
# Update the spec as you add or modify endpoints

# To view the API documentation locally, you can use any OpenAPI viewer
# or serve it through the running service at /api/v1/openapi
```

## Community

### Communication Channels

- **Issues**: GitHub Issues for bug reports and feature requests
- **Discussions**: GitHub Discussions for general questions
- **Pull Requests**: GitHub PRs for code contributions

### Getting Help

1. **Check Existing Issues**: Search for similar problems
2. **Read Documentation**: Review README and API docs
3. **Ask Questions**: Create a GitHub Discussion
4. **Join Community**: Engage with other contributors

### Recognition

Contributors are recognized in:
- **Release Notes**: Major contributions highlighted
- **Contributors**: GitHub automatically tracks contributors
- **Acknowledgments**: Special recognition for significant contributions

## License

By contributing to Eval Hub, you agree that your contributions will be licensed under the Apache License 2.0.

---

Thank you for contributing to Eval Hub! Your efforts help improve ML evaluation capabilities for the entire community.
