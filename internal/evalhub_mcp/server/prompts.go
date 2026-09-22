package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.yaml.in/yaml/v4"
)

const (
	PromptNameEDDWorkflow      = "edd_workflow"
	PromptNameEvaluateModel    = "evaluate_model"
	PromptNameCompareRuns      = "compare_runs"
	PromptNameDesignCollection = "design_collection"

	ApplicationTypeRAG        = "rag"
	ApplicationTypeAgent      = "agent"
	ApplicationTypeSafety     = "safety"
	ApplicationTypeClassifier = "classifier"

	ArgNameApplicationType      = "application_type"
	ArgNameModelURL             = "model_url"
	ArgNameBenchmarkPreferences = "benchmark_preferences"
	ArgNameJobIds               = "job_ids"
	ArgNameEvaluationGoal       = "evaluation_goal"
	ArgNameProviderFilter       = "provider_filter"
	ArgNameMaxBenchmarks        = "max_benchmarks"
	ArgNameStrictness           = "strictness"

	ArgNameGuidelineDefine         = "define"
	ArgNameGuidelineMeasure        = "measure"
	ArgNameGuidelineIterate        = "iterate"
	ArgNameGuidelineStartingPrompt = "starting_prompt"

	defaultStrictness    = "moderate"
	defaultMaxBenchmarks = 12
)

// For now the yaml files are embedded here, but in the future we should load them from config maps if needed

//go:embed prompts/prompts.yaml
var promptsYAML []byte

//go:embed prompts/guidance.yaml
var guidanceYAML []byte

var validApplicationTypes = []string{ApplicationTypeRAG, ApplicationTypeAgent, ApplicationTypeSafety, ApplicationTypeClassifier}

type promptConfig struct {
	Description string              `yaml:"description"`
	Arguments   promptArguments     `yaml:"arguments"`
	Result      *promptResultConfig `yaml:"result"`
}

type promptArgument struct {
	Name        string `yaml:"-"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required,omitempty"`
}

// promptArguments preserves the order in which arguments are declared in the
// YAML mapping. A plain map[string]*promptArgument would surface arguments in a
// non-deterministic order, so we decode the mapping node manually.
type promptArguments []*promptArgument

func (a *promptArguments) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("prompt arguments must be a mapping, got node kind %d", node.Kind)
	}
	result := make(promptArguments, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		arg := &promptArgument{Name: keyNode.Value}
		if err := valNode.Decode(arg); err != nil {
			return fmt.Errorf("decoding prompt argument %q: %w", keyNode.Value, err)
		}
		result = append(result, arg)
	}
	*a = result
	return nil
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
	for _, argument := range p.Arguments {
		arguments = append(arguments, &mcp.PromptArgument{
			Name:        argument.Name,
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

func registerPrompts(srv *mcp.Server, ds EvalHubDiscovery, logger *slog.Logger) error {
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
		case PromptNameDesignCollection:
			handler = designCollectionHandler(prompt.Result, ds, logger)
		default:
			return fmt.Errorf("prompt %q not found", name)
		}
		srv.AddPrompt(prompt.ToMCPPrompt(name), handler)
	}

	return nil
}

func eddWorkflowHandler(result *promptResultConfig, eddGuidance map[string]eddPhaseGuidance, logger *slog.Logger) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		log := requestLogger(ctx, logger)
		appType := req.Params.Arguments[ArgNameApplicationType]
		log.Debug(fmt.Sprintf("%s called", PromptNameEDDWorkflow), ArgNameApplicationType, appType)

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
		log := requestLogger(ctx, logger)
		modelURL := strings.TrimSpace(req.Params.Arguments[ArgNameModelURL])
		benchmarkPrefs := strings.TrimSpace(req.Params.Arguments[ArgNameBenchmarkPreferences])
		log.Debug(fmt.Sprintf("%s called", PromptNameEvaluateModel), ArgNameModelURL, modelURL, ArgNameBenchmarkPreferences, benchmarkPrefs)

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
		log := requestLogger(ctx, logger)
		jobIDsRaw := req.Params.Arguments[ArgNameJobIds]
		jobIDs := parseJobIDs(jobIDsRaw)
		log.Debug(fmt.Sprintf("%s called", PromptNameCompareRuns), ArgNameJobIds, jobIDsRaw)

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

var validStrictness = []string{"lenient", "moderate", "strict"}

func designCollectionHandler(result *promptResultConfig, ds EvalHubDiscovery, logger *slog.Logger) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		log := requestLogger(ctx, logger)
		ds := evalHubDiscoveryForRequest(ctx, ds, logger)

		goal := strings.TrimSpace(req.Params.Arguments[ArgNameEvaluationGoal])
		providerFilter := strings.TrimSpace(req.Params.Arguments[ArgNameProviderFilter])
		maxBenchmarksRaw := strings.TrimSpace(req.Params.Arguments[ArgNameMaxBenchmarks])
		strictness := strings.TrimSpace(req.Params.Arguments[ArgNameStrictness])

		log.Debug(fmt.Sprintf("%s called", PromptNameDesignCollection),
			ArgNameEvaluationGoal, goal,
			ArgNameProviderFilter, providerFilter,
			ArgNameMaxBenchmarks, maxBenchmarksRaw,
			ArgNameStrictness, strictness,
		)

		description, messages, err := buildDesignCollection(ds, result, goal, providerFilter, maxBenchmarksRaw, strictness)
		if err != nil {
			return nil, err
		}

		return &mcp.GetPromptResult{
			Description: description,
			Messages:    messages,
		}, nil
	}
}

// buildDesignCollection validates the design inputs and assembles the benchmark
// catalog and calibration guidance shared by the design_collection prompt and
// tool. It returns the (templated) description and the prompt messages, or an
// error for invalid input or an empty catalog. maxBenchmarksRaw is accepted as a
// string so both callers (prompt arguments and tool input) can share parsing.
func buildDesignCollection(ds EvalHubDiscovery, result *promptResultConfig, goal, providerFilter, maxBenchmarksRaw, strictness string) (string, []*mcp.PromptMessage, error) {
	goal = strings.TrimSpace(goal)
	providerFilter = strings.TrimSpace(providerFilter)
	maxBenchmarksRaw = strings.TrimSpace(maxBenchmarksRaw)
	strictness = strings.TrimSpace(strictness)

	if goal == "" {
		return "", nil, fmt.Errorf("%s is required", ArgNameEvaluationGoal)
	}

	maxBenchmarks := defaultMaxBenchmarks
	if maxBenchmarksRaw != "" {
		if n, err := strconv.Atoi(maxBenchmarksRaw); err == nil && n > 0 {
			maxBenchmarks = n
		}
	}

	if strictness == "" {
		strictness = defaultStrictness
	} else if !isValidStrictness(strictness) {
		return "", nil, fmt.Errorf("invalid %s %q; valid values: %s", ArgNameStrictness, strictness, strings.Join(validStrictness, ", "))
	}

	benchmarkCatalog, benchmarkCount, err := buildBenchmarkCatalog(ds, providerFilter)
	if err != nil {
		return "", nil, fmt.Errorf("fetching benchmark catalog: %w", err)
	}
	if benchmarkCount == 0 {
		if providerFilter != "" {
			return "", nil, fmt.Errorf("benchmark catalog is empty for provider filter %q; check that the eval-hub service has these providers loaded", providerFilter)
		}
		return "", nil, fmt.Errorf("benchmark catalog is empty; check that the eval-hub service has providers loaded")
	}

	collectionExamples, err := buildCollectionExamples(ds, providerFilter)
	if err != nil {
		return "", nil, fmt.Errorf("fetching collection examples: %w", err)
	}

	var optionsParts []string
	if providerFilter != "" {
		optionsParts = append(optionsParts, fmt.Sprintf("Provider filter: %s", providerFilter))
	}
	optionsParts = append(optionsParts, fmt.Sprintf("Max benchmarks: %s", strconv.Itoa(maxBenchmarks)))
	optionsParts = append(optionsParts, fmt.Sprintf("Strictness: %s", strictness))
	optionsSummary := strings.Join(optionsParts, " | ")

	messages := result.ToMCPPromptMessages(
		"",
		ArgNameEvaluationGoal, goal,
		"options_summary", optionsSummary,
		ArgNameStrictness, strictness,
		"benchmark_catalog", benchmarkCatalog,
		"collection_examples", collectionExamples,
	)
	if messages == nil {
		return "", nil, fmt.Errorf("no messages found for design_collection prompt")
	}

	description := replaceTemplateVariables(result.Description, ArgNameEvaluationGoal, goal)
	return description, messages, nil
}

// promptMessagesText joins the text content of prompt messages into a single
// document, used to surface the design_collection guidance as tool output.
func promptMessagesText(messages []*mcp.PromptMessage) string {
	parts := make([]string, 0, len(messages))
	for _, m := range messages {
		if tc, ok := m.Content.(*mcp.TextContent); ok && tc.Text != "" {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func isValidStrictness(s string) bool {
	for _, v := range validStrictness {
		if s == v {
			return true
		}
	}
	return false
}

type benchmarkCatalogEntry struct {
	ID          string   `json:"id"`
	ProviderID  string   `json:"provider_id"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Metrics     []string `json:"metrics,omitempty"`
}

func buildBenchmarkCatalog(ds EvalHubDiscovery, providerFilter string) (string, int, error) {
	var allowedProviders map[string]struct{}
	if providerFilter != "" {
		allowedProviders = make(map[string]struct{})
		for p := range strings.SplitSeq(providerFilter, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				allowedProviders[p] = struct{}{}
			}
		}
	}

	providers, err := allProviders(ds)
	if err != nil {
		return "", 0, err
	}

	entries := make([]benchmarkCatalogEntry, 0)
	for _, p := range providers {
		if allowedProviders != nil {
			if _, ok := allowedProviders[p.Resource.ID]; !ok {
				continue
			}
		}
		for _, b := range p.Benchmarks {
			entry := benchmarkCatalogEntry{
				ID:          b.ID,
				ProviderID:  p.Resource.ID,
				Name:        b.Name,
				Description: b.Description,
				Tags:        b.Tags,
			}
			if len(b.Metrics) > 0 {
				entry.Metrics = b.Metrics
			}
			entries = append(entries, entry)
		}
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return "", 0, fmt.Errorf("marshalling benchmark catalog: %w", err)
	}
	return string(data), len(entries), nil
}

type collectionExample struct {
	ID           string                          `json:"id"`
	Name         string                          `json:"name"`
	Category     string                          `json:"category,omitempty"`
	Description  string                          `json:"description,omitempty"`
	Tags         []string                        `json:"tags,omitempty"`
	PassCriteria *api.PassCriteria               `json:"pass_criteria,omitempty"`
	Benchmarks   []api.CollectionBenchmarkConfig `json:"benchmarks"`
}

func buildCollectionExamples(ds EvalHubDiscovery, providerFilter string) (string, error) {
	var allowedProviders map[string]struct{}
	if providerFilter != "" {
		allowedProviders = make(map[string]struct{})
		for p := range strings.SplitSeq(providerFilter, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				allowedProviders[p] = struct{}{}
			}
		}
	}

	collections, err := allCollections(ds)
	if err != nil {
		return "", err
	}

	examples := make([]collectionExample, 0)
	for _, c := range collections {
		if c.Resource.Owner != "system" {
			continue
		}
		benchmarks := c.Benchmarks
		if allowedProviders != nil {
			benchmarks = filterBenchmarksByProvider(benchmarks, allowedProviders)
			if len(benchmarks) == 0 {
				continue
			}
		}
		examples = append(examples, collectionExample{
			ID:           c.Resource.ID,
			Name:         c.Name,
			Category:     c.Category,
			Description:  c.Description,
			Tags:         c.Tags,
			PassCriteria: c.PassCriteria,
			Benchmarks:   benchmarks,
		})
	}

	data, err := json.MarshalIndent(examples, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshalling collection examples: %w", err)
	}
	return string(data), nil
}

func filterBenchmarksByProvider(benchmarks []api.CollectionBenchmarkConfig, allowed map[string]struct{}) []api.CollectionBenchmarkConfig {
	var filtered []api.CollectionBenchmarkConfig
	for _, b := range benchmarks {
		if _, ok := allowed[b.ProviderID]; ok {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

// allProviders paginates through all provider pages from the discovery source.
func allProviders(ds EvalHubDiscovery) ([]api.ProviderResource, error) {
	pageSize := evalhubclient.DefaultListPageLimit
	var all []api.ProviderResource
	for offset := 0; ; offset += pageSize {
		list, err := ds.ListProviders(evalhubclient.WithLimit(pageSize), evalhubclient.WithOffset(offset))
		if err != nil {
			return nil, err
		}
		all = append(all, list.Items...)
		if len(all) >= list.TotalCount || len(list.Items) < pageSize {
			break
		}
	}
	return all, nil
}

// allCollections paginates through all collection pages from the discovery source.
func allCollections(ds EvalHubDiscovery) ([]api.CollectionResource, error) {
	pageSize := evalhubclient.DefaultListPageLimit
	var all []api.CollectionResource
	for offset := 0; ; offset += pageSize {
		list, err := ds.ListCollections(evalhubclient.WithLimit(pageSize), evalhubclient.WithOffset(offset))
		if err != nil {
			return nil, err
		}
		all = append(all, list.Items...)
		if len(all) >= list.TotalCount || len(list.Items) < pageSize {
			break
		}
	}
	return all, nil
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
	for i := 0; i+1 < len(variables); i += 2 {
		s = strings.ReplaceAll(s, "{"+variables[i]+"}", variables[i+1])
	}
	return s
}
