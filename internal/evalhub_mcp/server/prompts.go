package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var validApplicationTypes = []string{"rag", "agent", "safety", "classifier"}

func registerPrompts(srv *mcp.Server, logger *slog.Logger) {
	srv.AddPrompt(&mcp.Prompt{
		Name:        "edd_workflow",
		Description: "Structured guidance for the Evaluation-Driven Development cycle (Define → Measure → Iterate), tailored to a specific application type.",
		Arguments: []*mcp.PromptArgument{
			{
				Name:        "application_type",
				Description: "The type of application to evaluate: rag, agent, safety, or classifier",
				Required:    true,
			},
		},
	}, eddWorkflowHandler(logger))

	srv.AddPrompt(&mcp.Prompt{
		Name:        "evaluate_model",
		Description: "Step-by-step guidance to evaluate a model: collect model URL, select benchmarks, configure experiment, submit job, and monitor results.",
		Arguments: []*mcp.PromptArgument{
			{
				Name:        "model_url",
				Description: "URL of the model inference endpoint (skips the model collection step if provided)",
			},
			{
				Name:        "benchmark_preferences",
				Description: "Preferences for benchmark selection such as category or focus area (e.g. reasoning, safety, general)",
			},
		},
	}, evaluateModelHandler(logger))

	srv.AddPrompt(&mcp.Prompt{
		Name:        "compare_runs",
		Description: "Guidance for comparing two or more evaluation jobs: select jobs, fetch results, compare metrics, and summarize findings.",
		Arguments: []*mcp.PromptArgument{
			{
				Name:        "job_ids",
				Description: "Comma-separated list of evaluation job IDs to compare (skips the job selection step if provided)",
			},
		},
	}, compareRunsHandler(logger))
}

func eddWorkflowHandler(logger *slog.Logger) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		appType := req.Params.Arguments["application_type"]
		logger.Debug("edd_workflow called", "application_type", appType)

		if appType == "" {
			return nil, fmt.Errorf("application_type is required; valid values: %s", strings.Join(validApplicationTypes, ", "))
		}

		guidance, ok := eddGuidance[appType]
		if !ok {
			return nil, fmt.Errorf("invalid application_type %q; valid values: %s", appType, strings.Join(validApplicationTypes, ", "))
		}

		return &mcp.GetPromptResult{
			Description: fmt.Sprintf("Evaluation-Driven Development workflow for %s applications", appType),
			Messages: []*mcp.PromptMessage{
				{
					Role:    "user",
					Content: &mcp.TextContent{Text: fmt.Sprintf("I want to follow an Evaluation-Driven Development (EDD) workflow for my %s application.", appType)},
				},
				{
					Role: "assistant",
					Content: &mcp.TextContent{Text: fmt.Sprintf(
						"I'll guide you through the Evaluation-Driven Development cycle for your %s application. EDD follows three phases: **Define → Measure → Iterate**.\n\n"+
							"## Phase 1: Define\n%s\n\n"+
							"## Phase 2: Measure\n%s\n\n"+
							"## Phase 3: Iterate\n%s\n\n"+
							"Let's start with Phase 1. %s",
						appType,
						guidance.define,
						guidance.measure,
						guidance.iterate,
						guidance.startingPrompt,
					)},
				},
			},
		}, nil
	}
}

func evaluateModelHandler(logger *slog.Logger) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		modelURL := req.Params.Arguments["model_url"]
		benchmarkPrefs := req.Params.Arguments["benchmark_preferences"]
		logger.Debug("evaluate_model called", "model_url", modelURL, "benchmark_preferences", benchmarkPrefs)

		var messages []*mcp.PromptMessage

		if modelURL == "" {
			messages = append(messages, &mcp.PromptMessage{
				Role:    "user",
				Content: &mcp.TextContent{Text: "I want to evaluate a model. Help me through the process step by step."},
			}, &mcp.PromptMessage{
				Role: "assistant",
				Content: &mcp.TextContent{Text: "I'll guide you through the model evaluation process.\n\n" +
					"**Step 1: Identify your model**\n" +
					"Please provide the URL of the model inference endpoint you want to evaluate. " +
					"This should be the API endpoint where the model is serving predictions (e.g. http://my-model:8080/v1)."},
			})
		} else {
			messages = append(messages, &mcp.PromptMessage{
				Role:    "user",
				Content: &mcp.TextContent{Text: fmt.Sprintf("I want to evaluate the model at %s. Help me through the process step by step.", modelURL)},
			}, &mcp.PromptMessage{
				Role: "assistant",
				Content: &mcp.TextContent{Text: fmt.Sprintf("I'll guide you through evaluating the model at `%s`.\n\n"+
					"Model URL confirmed. Let's proceed to benchmark selection.", modelURL)},
			})
		}

		benchmarkStep := "**Step 2: Select benchmarks**\n" +
			"Choose which benchmarks to run against your model. "
		if benchmarkPrefs != "" {
			benchmarkStep += fmt.Sprintf("Based on your preferences (%s), I'll help narrow down the options. ", benchmarkPrefs)
		}
		benchmarkStep += "Use the `evalhub://benchmarks` resource to browse available benchmarks, " +
			"or `evalhub://collections` for curated benchmark suites. " +
			"You can filter benchmarks by label (e.g. `evalhub://benchmarks?label=safety`).\n\n" +
			"**Step 3: Configure experiment**\n" +
			"Optionally set up MLflow experiment tracking with a name and tags for organizing your evaluation results.\n\n" +
			"**Step 4: Submit evaluation job**\n" +
			"Once benchmarks are selected, use the `submit_evaluation` tool to create the evaluation job. " +
			"Provide the model URL, selected benchmarks or collection, and optional experiment configuration.\n\n" +
			"**Step 5: Monitor results**\n" +
			"After submission, use `get_job_status` to track progress. " +
			"The job will report per-benchmark status and overall completion percentage. " +
			"Once complete, review the results to understand your model's performance."

		messages = append(messages, &mcp.PromptMessage{
			Role:    "user",
			Content: &mcp.TextContent{Text: "What's next?"},
		}, &mcp.PromptMessage{
			Role:    "assistant",
			Content: &mcp.TextContent{Text: benchmarkStep},
		})

		return &mcp.GetPromptResult{
			Description: "Step-by-step model evaluation workflow",
			Messages:    messages,
		}, nil
	}
}

func compareRunsHandler(logger *slog.Logger) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		jobIDsRaw := req.Params.Arguments["job_ids"]
		jobIDs := parseJobIDs(jobIDsRaw)
		logger.Debug("compare_runs called", "job_ids", jobIDsRaw)

		var messages []*mcp.PromptMessage

		if len(jobIDs) == 0 {
			messages = append(messages, &mcp.PromptMessage{
				Role:    "user",
				Content: &mcp.TextContent{Text: "I want to compare evaluation results across multiple runs."},
			}, &mcp.PromptMessage{
				Role: "assistant",
				Content: &mcp.TextContent{Text: "I'll help you compare evaluation runs.\n\n" +
					"**Step 1: Select jobs to compare**\n" +
					"First, let's identify which evaluation jobs you want to compare. " +
					"Use the `evalhub://jobs` resource to list available jobs, " +
					"or filter by status with `evalhub://jobs?status=completed` to see only finished evaluations. " +
					"Select two or more job IDs to proceed."},
			})
		} else {
			messages = append(messages, &mcp.PromptMessage{
				Role:    "user",
				Content: &mcp.TextContent{Text: fmt.Sprintf("I want to compare evaluation jobs: %s", strings.Join(jobIDs, ", "))},
			}, &mcp.PromptMessage{
				Role: "assistant",
				Content: &mcp.TextContent{Text: fmt.Sprintf("I'll compare the evaluation results for jobs: %s.\n\n"+
					"**Step 1: Fetch results**\n"+
					"Let me retrieve the results for each job. Use the `evalhub://jobs/{id}` resource "+
					"for each job to get detailed status and benchmark results.",
					strings.Join(jobIDs, ", "))},
			})
		}

		messages = append(messages, &mcp.PromptMessage{
			Role:    "user",
			Content: &mcp.TextContent{Text: "How should I analyze the comparison?"},
		}, &mcp.PromptMessage{
			Role: "assistant",
			Content: &mcp.TextContent{Text: "**Step 2: Compare metrics**\n" +
				"For each shared benchmark across the selected jobs, compare the key metrics:\n" +
				"- Identify which model performed better on each benchmark\n" +
				"- Look for significant differences vs. marginal ones\n" +
				"- Note any benchmarks where one model failed but another succeeded\n\n" +
				"**Step 3: Summarize findings**\n" +
				"Synthesize the comparison into actionable insights:\n" +
				"- Overall winner based on aggregate performance\n" +
				"- Category-level strengths (e.g. one model may excel at reasoning while another at safety)\n" +
				"- Recommendations for which model to use based on your priorities\n" +
				"- Areas where both models underperform, suggesting need for further iteration"},
		})

		return &mcp.GetPromptResult{
			Description: "Evaluation job comparison workflow",
			Messages:    messages,
		}, nil
	}
}

func parseJobIDs(raw string) []string {
	var ids []string
	for _, id := range strings.Split(raw, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

type eddPhaseGuidance struct {
	define         string
	measure        string
	iterate        string
	startingPrompt string
}

var eddGuidance = map[string]eddPhaseGuidance{
	"rag": {
		define: "Define what success looks like for your RAG pipeline:\n" +
			"- Identify the types of questions your system must answer\n" +
			"- Define expected retrieval quality (precision, recall of relevant documents)\n" +
			"- Set accuracy targets for generated answers (faithfulness, relevance)\n" +
			"- Establish latency and throughput requirements",
		measure: "Select benchmarks that evaluate RAG-specific capabilities:\n" +
			"- Use benchmarks tagged with `rag` to test retrieval and generation quality\n" +
			"- Evaluate faithfulness (does the answer stick to retrieved context?)\n" +
			"- Measure answer relevance and completeness\n" +
			"- Test handling of unanswerable questions and conflicting sources",
		iterate: "Improve your RAG pipeline based on evaluation results:\n" +
			"- Tune retrieval parameters (chunk size, top-k, similarity thresholds)\n" +
			"- Experiment with different embedding models\n" +
			"- Adjust the generation prompt template\n" +
			"- Add re-ranking or filtering stages to improve context quality\n" +
			"- Re-evaluate after each change to track improvements",
		startingPrompt: "What types of questions does your RAG system need to handle, and what are your quality targets?",
	},
	"agent": {
		define: "Define evaluation criteria for your agent:\n" +
			"- Identify the tasks your agent must accomplish\n" +
			"- Define success criteria for tool use (correct tool selection, parameter accuracy)\n" +
			"- Set expectations for multi-step reasoning and planning\n" +
			"- Establish safety boundaries (actions the agent must never take)",
		measure: "Select benchmarks that evaluate agent capabilities:\n" +
			"- Use benchmarks tagged with `agents` to test tool use and reasoning\n" +
			"- Evaluate task completion rates across different complexity levels\n" +
			"- Measure planning quality and step efficiency\n" +
			"- Test error recovery and graceful fallback behavior",
		iterate: "Improve your agent based on evaluation results:\n" +
			"- Refine tool descriptions and parameter schemas\n" +
			"- Adjust the system prompt to improve planning behavior\n" +
			"- Add or remove available tools based on task requirements\n" +
			"- Implement guardrails for identified failure modes\n" +
			"- Re-evaluate after each change to validate improvements",
		startingPrompt: "What tasks does your agent need to perform, and what tools does it have access to?",
	},
	"safety": {
		define: "Define safety requirements for your model:\n" +
			"- Identify the types of harmful content your model must avoid generating\n" +
			"- Define compliance requirements (regulatory, organizational policies)\n" +
			"- Set thresholds for acceptable false positive rates\n" +
			"- Establish categories of concern (toxicity, bias, privacy, misinformation)",
		measure: "Select safety-specific benchmarks:\n" +
			"- Use benchmarks tagged with `safety` for comprehensive safety evaluation\n" +
			"- Test resistance to adversarial attacks and jailbreak attempts\n" +
			"- Evaluate bias across demographic groups and protected attributes\n" +
			"- Measure toxicity and harmful content generation rates\n" +
			"- Use providers like Garak for specialized security testing",
		iterate: "Improve model safety based on evaluation results:\n" +
			"- Add safety-specific system prompts and guardrails\n" +
			"- Fine-tune with safety-aligned datasets\n" +
			"- Implement content filtering layers\n" +
			"- Add input validation and output sanitization\n" +
			"- Re-evaluate after each change to ensure safety without degrading utility",
		startingPrompt: "What are the primary safety concerns for your deployment, and what compliance requirements apply?",
	},
	"classifier": {
		define: "Define evaluation criteria for your classifier:\n" +
			"- Identify the classification task and target classes\n" +
			"- Define per-class accuracy and precision/recall requirements\n" +
			"- Set thresholds for edge cases and ambiguous inputs\n" +
			"- Establish requirements for confidence calibration",
		measure: "Select benchmarks that evaluate classification quality:\n" +
			"- Use benchmarks aligned with your classification domain\n" +
			"- Evaluate overall accuracy, precision, recall, and F1 scores\n" +
			"- Measure performance across class imbalances\n" +
			"- Test robustness to input variations and adversarial examples\n" +
			"- Assess confidence calibration (are predicted probabilities reliable?)",
		iterate: "Improve your classifier based on evaluation results:\n" +
			"- Adjust classification prompts or fine-tuning data\n" +
			"- Add few-shot examples for underperforming classes\n" +
			"- Implement confidence thresholds and fallback strategies\n" +
			"- Balance training data to address class imbalance\n" +
			"- Re-evaluate after each change to confirm improvements",
		startingPrompt: "What is your classification task, and which classes are most critical to get right?",
	},
}
