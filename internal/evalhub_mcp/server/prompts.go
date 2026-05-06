package server

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.yaml.in/yaml/v4"
)

const (
	PromptNameEDDWorkflow   = "edd_workflow"
	PromptNameEvaluateModel = "evaluate_model"
	PromptNameCompareRuns   = "compare_runs"

	ApplicationTypeRAG        = "rag"
	ApplicationTypeAgent      = "agent"
	ApplicationTypeSafety     = "safety"
	ApplicationTypeClassifier = "classifier"

	ArgNameApplicationType      = "application_type"
	ArgNameModelURL             = "model_url"
	ArgNameBenchmarkPreferences = "benchmark_preferences"
	ArgNameJobIds               = "job_ids"

	ArgNameGuidelineDefine         = "define"
	ArgNameGuidelineMeasure        = "measure"
	ArgNameGuidelineIterate        = "iterate"
	ArgNameGuidelineStartingPrompt = "starting_prompt"
)

// For now the yaml files are embedded here, but in the future we should load them from config maps if needed

//go:embed prompts/prompts.yaml
var promptsYAML []byte

//go:embed prompts/guidance.yaml
var guidanceYAML []byte

var validApplicationTypes = []string{ApplicationTypeRAG, ApplicationTypeAgent, ApplicationTypeSafety, ApplicationTypeClassifier}

type promptConfig struct {
	Description string                     `yaml:"description"`
	Arguments   map[string]*promptArgument `yaml:"arguments"`
	Result      *promptResultConfig        `yaml:"result"`
}

type promptArgument struct {
	Description string `yaml:"description"`
	Required    bool   `yaml:"required,omitempty"`
}

type promptResultConfig struct {
	Description string                       `yaml:"description"`
	Messages    map[string]map[string]string `yaml:"messages"`
}

func (p *promptConfig) ToMCPPrompt(name string) *mcp.Prompt {
	return &mcp.Prompt{
		Name:        name,
		Description: p.Description,
		Arguments:   p.ToMCPPromptArguments(),
	}
}

func (p *promptConfig) ToMCPPromptArguments() []*mcp.PromptArgument {
	arguments := make([]*mcp.PromptArgument, 0, len(p.Arguments))
	for name, argument := range p.Arguments {
		arguments = append(arguments, &mcp.PromptArgument{
			Name:        name,
			Description: argument.Description,
			Required:    argument.Required,
		})
	}
	return arguments
}

func (p *promptResultConfig) ToMCPPromptMessages(group string, args ...string) []*mcp.PromptMessage {
	if group == "" {
		group = "any"
	}
	messageConfigs, ok := p.Messages[group]
	if !ok {
		return nil
	}
	messages := make([]*mcp.PromptMessage, 0, len(messageConfigs))
	for _, roleName := range []string{"user", "assistant"} {
		content, present := messageConfigs[roleName]
		if !present {
			// this should never happen, but we'll handle it gracefully
			continue
		}
		messages = append(messages, &mcp.PromptMessage{
			Role:    mcp.Role(roleName),
			Content: &mcp.TextContent{Text: replaceTemplateVariables(content, args...)},
		})
	}
	return messages
}

type eddPhaseGuidance struct {
	Define         string `yaml:"define"`
	Measure        string `yaml:"measure"`
	Iterate        string `yaml:"iterate"`
	StartingPrompt string `yaml:"starting_prompt"`
}

func (g *eddPhaseGuidance) IsValid() bool {
	return g != nil && g.Define != "" && g.Measure != "" && g.Iterate != "" && g.StartingPrompt != ""
}

func registerPrompts(srv *mcp.Server, logger *slog.Logger) error {
	eddGuidance, err := loadGuidance()
	if err != nil {
		return err
	}

	prompts, err := loadPrompts()
	if err != nil {
		return err
	}

	for name, prompt := range prompts {
		var handler mcp.PromptHandler
		switch name {
		case PromptNameEDDWorkflow:
			handler = eddWorkflowHandler(prompt.Result, eddGuidance, logger)
		case PromptNameEvaluateModel:
			handler = evaluateModelHandler(prompt.Result, logger)
		case PromptNameCompareRuns:
			handler = compareRunsHandler(prompt.Result, logger)
		default:
			return fmt.Errorf("prompt %q not found", name)
		}
		srv.AddPrompt(prompt.ToMCPPrompt(name), handler)
	}

	return nil
}

func eddWorkflowHandler(result *promptResultConfig, eddGuidance map[string]eddPhaseGuidance, logger *slog.Logger) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		appType := req.Params.Arguments[ArgNameApplicationType]
		logger.Debug(fmt.Sprintf("%s called", PromptNameEDDWorkflow), ArgNameApplicationType, appType)

		if appType == "" {
			return nil, fmt.Errorf("%s is required; valid values: %s", ArgNameApplicationType, strings.Join(validApplicationTypes, ", "))
		}

		guidance, ok := eddGuidance[appType]
		if !ok {
			return nil, fmt.Errorf("invalid %s %q; valid values: %s", ArgNameApplicationType, appType, strings.Join(validApplicationTypes, ", "))
		}

		messages := result.ToMCPPromptMessages(
			"",
			ArgNameApplicationType, appType,
			ArgNameGuidelineDefine, guidance.Define,
			ArgNameGuidelineMeasure, guidance.Measure,
			ArgNameGuidelineIterate, guidance.Iterate,
			ArgNameGuidelineStartingPrompt, guidance.StartingPrompt,
		)
		if messages == nil {
			return nil, fmt.Errorf("no messages found for empty group")
		}

		return &mcp.GetPromptResult{
			Description: replaceTemplateVariables(
				result.Description, ArgNameApplicationType, appType,
			),
			Messages: messages,
		}, nil
	}
}

func evaluateModelHandler(result *promptResultConfig, logger *slog.Logger) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		modelURL := strings.TrimSpace(req.Params.Arguments[ArgNameModelURL])
		benchmarkPrefs := strings.TrimSpace(req.Params.Arguments[ArgNameBenchmarkPreferences])
		logger.Debug(fmt.Sprintf("%s called", PromptNameEvaluateModel), ArgNameModelURL, modelURL, ArgNameBenchmarkPreferences, benchmarkPrefs)

		var messages []*mcp.PromptMessage
		if modelURL == "" {
			messages = result.ToMCPPromptMessages("no_model")
			if messages == nil {
				return nil, fmt.Errorf("no messages found for group %q", "no_model")
			}
		} else {
			messages = result.ToMCPPromptMessages("with_model", ArgNameModelURL, modelURL)
			if messages == nil {
				return nil, fmt.Errorf("no messages found for group %q", "with_model")
			}
		}

		var benchmarkMessages []*mcp.PromptMessage
		if benchmarkPrefs == "" {
			benchmarkMessages = result.ToMCPPromptMessages(
				"benchmark_selection_no_preferences",
			)
			if benchmarkMessages == nil {
				return nil, fmt.Errorf("no messages found for group %q", "benchmark_selection_no_preferences")
			}
		} else {
			benchmarkMessages = result.ToMCPPromptMessages(
				"benchmark_selection_with_preferences",
				ArgNameBenchmarkPreferences, benchmarkPrefs,
			)
			if benchmarkMessages == nil {
				return nil, fmt.Errorf("no messages found for group %q", "benchmark_selection_with_preferences")
			}
		}

		return &mcp.GetPromptResult{
			Description: result.Description,
			Messages:    append(messages, benchmarkMessages...),
		}, nil
	}
}

func compareRunsHandler(result *promptResultConfig, logger *slog.Logger) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		jobIDsRaw := req.Params.Arguments[ArgNameJobIds]
		jobIDs := parseJobIDs(jobIDsRaw)
		logger.Debug(fmt.Sprintf("%s called", PromptNameCompareRuns), ArgNameJobIds, jobIDsRaw)

		var messages []*mcp.PromptMessage
		if jobIDs == nil {
			messages = result.ToMCPPromptMessages(
				"no_jobs",
			)
			if messages == nil {
				return nil, fmt.Errorf("no messages found for group %q", "no_jobs")
			}
		} else if len(jobIDs) < 2 {
			return nil, fmt.Errorf("%s must contain at least two job IDs", ArgNameJobIds)
		} else {
			messages = result.ToMCPPromptMessages(
				"with_jobs",
				ArgNameJobIds, strings.Join(jobIDs, ", "),
			)
			if messages == nil {
				return nil, fmt.Errorf("no messages found for group %q", "with_jobs")
			}
		}

		comparisonMessages := result.ToMCPPromptMessages(
			"comparison",
		)
		if comparisonMessages == nil {
			return nil, fmt.Errorf("no messages found for group %q", "comparison")
		}

		return &mcp.GetPromptResult{
			Description: result.Description,
			Messages:    append(messages, comparisonMessages...),
		}, nil
	}
}

func loadPrompts() (map[string]promptConfig, error) {
	prompts := make(map[string]promptConfig)
	err := yaml.Unmarshal(promptsYAML, &prompts)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal prompts: %w", err)
	}
	return prompts, nil
}

func loadGuidance() (map[string]eddPhaseGuidance, error) {
	var eddGuidance map[string]eddPhaseGuidance
	if err := yaml.Unmarshal(guidanceYAML, &eddGuidance); err != nil {
		return nil, fmt.Errorf("evalhub_mcp: load prompts/guidance.yaml: %w", err)
	}
	for _, k := range validApplicationTypes {
		if g, ok := eddGuidance[k]; !ok || !g.IsValid() {
			return nil, fmt.Errorf("evalhub_mcp: prompts/guidance.yaml missing or incomplete key %q", k)
		}
	}
	return eddGuidance, nil
}

func parseJobIDs(raw string) []string {
	var ids []string
	for id := range strings.SplitSeq(raw, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func replaceTemplateVariables(s string, variables ...string) string {
	for i := 0; i < len(variables); i += 2 {
		s = strings.ReplaceAll(s, "{"+variables[i]+"}", variables[i+1])
	}
	return s
}
