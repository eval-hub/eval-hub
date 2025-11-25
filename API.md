## Eval Control Plane API proposal

### High-Level Summary

The Thin API Router proposal creates a custom-built microservice focused solely on evaluation orchestration with minimal dependencies and maximum flexibility. This approach prioritizes lightweight operation and complete control over the orchestration logic. It offers optimal performance and customization at the cost of increased development effort.
The Router/Orch layer **will not** provide any evaluation/benchmark runtime capabilities, its sole purpose is to translate requests into backend executions and storage.

**Key Benefits**: Minimal footprint, maximum flexibility, full API/design control
**Key Challenges**: Higher development effort, manual ecosystem integration, feature scope responsibility

### Architecture Overview

This solution implements a dedicated Kubernetes-native control plane for evaluation that orchestrates underlying frameworks directly, providing lifecycle management and extensibility. The platform focuses on plug-in extensibility and provides out-of-the-box support for the currently and future available evaluation backends.

#### Core Components

##### 1\. Core API Service

- **Lightweight Router**: FastAPI-based service with minimal dependencies
- **Request Processor**: Schema validation and backend capability matching
- **Communication Layer**: Protocol adapters for different backend types (HTTP, gRPC, message queue)

##### 2\. Kubernetes-Native Control Plane

- **Operator-Based Management**: Dedicated controller, deployed as Kubernetes Operator managed by TrustyAI team
- **Unified Orchestration Layer**: Custom-built platform for managing multiple evaluation frameworks
- **Framework Discovery Service**: Lists all available evaluation capabilities

##### 3\. Framework Management & Extensibility

- **Built-in Framework Support**: Out-of-the-box support for LMEval, RAGAS, Garak and GuideLLM
- **Bring Your Own Framework (BYOF)**: Container images with standardized interfaces
- **Framework Registry**: Centralized catalog of available evaluation frameworks

##### 4\. Enterprise MLOps Integration

- **MLOps Traceability**: Full evaluation traces persisted in OCI-backed storage
- **Model Governance**: Automatic surfacing of evaluation metrics in Model Registry UI
- **Industry-Specific Collections**: Curated evaluation collections for healthcare, legal, finance domains

### Comprehensive API Analysis

The eval-hub implements a comprehensive REST API with versioned endpoints (`/api/v1/`) providing orchestration capabilities across multiple evaluation backends. The API follows OpenAPI 3.0 specification and supports both synchronous and asynchronous execution patterns.

#### API Base Configuration

- **Base URL**: `http://localhost:8000/api/v1/`
- **API Version**: v1
- **Content-Type**: `application/json`
- **Authentication**: Bearer token (configurable)
- **OpenAPI Spec**: Available at `/docs` (Swagger UI) and `/openapi.json`

---

## **Core API Endpoints**

### Health & Status Endpoints

#### **GET** `/health` \- Health Check

**Purpose**: Service health monitoring and dependency status **Response Model**: `HealthResponse`

```shell
curl -X GET "{{baseUrl}}/health"
```

**Response Example**:

```json
{
  "status": "healthy",
  "version": "0.1.0",
  "timestamp": "2025-01-15T10:30:00Z",
  "components": {
    "mlflow": {
      "status": "healthy",
      "tracking_uri": "http://mlflow:5000"
    }
  },
  "uptime_seconds": 3600.5,
  "active_evaluations": 3
}
```

**Commentary**: Provides comprehensive health status including external dependencies (MLFlow), current system load (active evaluations), and service uptime. Essential for monitoring and observability.

---

### Evaluation Management Endpoints

#### **POST** `/evaluations` \- Create Evaluation Request

**Purpose**: Submit evaluation jobs for execution across multiple backends and providers
**Response Model**: `EvaluationResponse`
**Status Code**: `202 ACCEPTED` (async) | `200 OK` (sync)

**Multiple Benchmarks with Same Provider:**

```shell
curl -X POST "{{baseUrl}}/evaluations?async_mode=true" \
-H "Content-Type: application/json" \
-d '{
  "model": {
    "server": "vllm",
    "name": "meta-llama/llama-3.1-8b",
    "configuration": {
      "temperature": 0.1,
      "max_tokens": 512,
      "top_p": 0.95
    }
  },
  "benchmarks": [
    {
      "benchmark_id": "arc_easy",
      "provider_id": "lm_evaluation_harness",
      "config": {
        "num_fewshot": 0,
        "limit": 1000,
        "batch_size": 16,
        "include_path": "./custom_prompts/arc_easy.yaml"
      }
    },
    {
      "benchmark_id": "mmlu",
      "provider_id": "lm_evaluation_harness",
      "config": {
        "num_fewshot": 5,
        "limit": null,
        "batch_size": 16,
        "include_path": "./custom_prompts/mmlu_cot.yaml",
        "fewshot_as_multiturn": false,
        "trust_remote_code": false
      }
    }
  ],
  "experiment_name": "llama-3.1-8b-reasoning-eval",
  "tags": {
    "environment": "production",
    "model_family": "llama-3.1",
    "evaluation_type": "reasoning"
  }
}'
```

**Mixed Providers in Single Request:**

```shell
curl -X POST "{{baseUrl}}/evaluations?async_mode=true" \
-H "Content-Type: application/json" \
-d '{
  "model": {
    "server": "vllm",
    "name": "meta-llama/llama-3.1-8b",
    "configuration": {
      "temperature": 0.1,
      "max_tokens": 512,
      "top_p": 0.95
    }
  },
  "benchmarks": [
    {
      "benchmark_id": "arc_easy",
      "provider_id": "lm_evaluation_harness",
      "config": {
        "num_fewshot": 0,
        "batch_size": 8,
        "device": "cuda:0"
      }
    },
    {
      "benchmark_id": "hellaswag",
      "provider_id": "lm_evaluation_harness",
      "config": {
        "num_fewshot": 10,
        "batch_size": 8
      }
    },
    {
      "benchmark_id": "faithfulness",
      "provider_id": "ragas",
      "config": {
        "dataset_path": "./data/rag_test_set.jsonl",
        "embeddings_model": "sentence-transformers/all-mpnet-base-v2",
        "retrieval_system": "vector_db",
        "chunk_size": 512,
        "top_k": 5
      }
    },
    {
      "benchmark_id": "answer_relevancy",
      "provider_id": "ragas",
      "config": {
        "dataset_path": "./data/rag_test_set.jsonl",
        "ground_truth_path": "./data/ground_truth.jsonl"
      }
    },
    {
      "benchmark_id": "prompt_injection",
      "provider_id": "garak",
      "config": {
        "max_attempts": 100,
        "confidence_threshold": 0.8,
        "scan_modules": ["encoding", "leakage"],
        "reporting_level": "detailed"
      }
    }
  ],
  "experiment_name": "comprehensive-model-assessment",
  "tags": {
    "evaluation_suite": "complete",
    "model_version": "v2.1.0"
  }
}'
```

**Response Example**:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "pending",
  "benchmarks": [
    {
      "benchmark_id": "arc_easy",
      "provider_id": "lm_evaluation_harness",
      "evaluation_id": "eval_001",
      "status": "pending",
      "created_at": "2025-01-15T10:30:00Z"
    },
    {
      "benchmark_id": "hellaswag",
      "provider_id": "lm_evaluation_harness",
      "evaluation_id": "eval_001_batch",
      "status": "pending",
      "batch_optimization": true,
      "created_at": "2025-01-15T10:30:00Z"
    },
    {
      "benchmark_id": "faithfulness",
      "provider_id": "ragas",
      "evaluation_id": "eval_002",
      "status": "pending",
      "created_at": "2025-01-15T10:30:00Z"
    },
    {
      "benchmark_id": "answer_relevancy",
      "provider_id": "ragas",
      "evaluation_id": "eval_003",
      "status": "pending",
      "created_at": "2025-01-15T10:30:00Z"
    },
    {
      "benchmark_id": "prompt_injection",
      "provider_id": "garak",
      "evaluation_id": "eval_004",
      "status": "pending",
      "created_at": "2025-01-15T10:30:00Z"
    }
  ],
  "provider_optimization": {
    "lm_evaluation_harness": {
      "benchmarks": 2,
      "jobs": 1,
      "batched": true
    },
    "ragas": {
      "benchmarks": 2,
      "jobs": 2,
      "batched": false
    },
    "garak": {
      "benchmarks": 1,
      "jobs": 1,
      "batched": false
    }
  },
  "total_benchmarks": 5,
  "total_jobs": 4,
  "experiment_id": "exp_12345",
  "experiment_url": "http://mlflow:5000/experiments/12345",
  "created_at": "2025-01-15T10:30:00Z"
}
```

**Commentary**:

The evaluation request uses a clean, flat structure for maximum simplicity and consistency:

- **Single Model Configuration**: One model applies to all benchmarks in the request
- **Flat Benchmark Array**: Direct list of benchmarks without nested groupings
- **Simple benchmark specification**: Each benchmark directly references `benchmark_id` \+ `provider_id`
- **Provider-specific parameters**: All provider-specific config goes in the benchmark's `config` object
- **Minimal nesting**: Just model \+ benchmarks array \+ metadata

**Key Features:**

- **Single Model Evaluation**: One model configuration applies to all benchmarks in the request
- **Direct provider mapping**: `"provider_id": "lm_evaluation_harness"` directly maps to available providers
- **Mixed Provider Support**: Since `provider_id` is specified per benchmark, requests can mix providers freely
- **Flat Structure**: No artificial grouping layers \- just specify what benchmarks to run
- **Provider-specific config**: Each benchmark config contains only parameters relevant to that provider
- **Simple Construction**: Clients build a straightforward list of benchmarks to execute
- **Batching**: API automatically groups compatible benchmarks
- **Async/sync modes**: Default async returns immediately with tracking IDs, sync blocks until completion
- **MLFlow integration**: Automatic experiment tracking and result persistence

**Design Rationale:** This flat structure eliminates unnecessary complexity. Since each benchmark specifies its `provider_id`, there's no need for grouping at the request level. The API handles provider optimization internally, while clients enjoy a simple "run these benchmarks on this model" interface. For organization needs, use collections for pre-curated sets or tags for custom grouping.


#### **GET** `/evaluations/jobs/{id}` \- Get Evaluation Status

**Purpose**: Get the status of an evaluation request **Response Model**: `EvaluationResponse`

```shell
curl -X GET "{{baseUrl}}/evaluations/jobs/550e8400-e29b-41d4-a716-446655440000"
```

**Response Example**:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "running",
  "progress": 0.65,
  "benchmarks": [
    {
      "benchmark_id": "arc_easy",
      "provider_id": "lm_evaluation_harness",
      "evaluation_id": "eval_001",
      "status": "running",
      "progress": 0.65,
      "started_at": "2025-01-15T10:31:00Z",
      "estimated_completion": "2025-01-15T10:45:00Z"
    },
    {
      "benchmark_id": "faithfulness",
      "provider_id": "ragas",
      "evaluation_id": "eval_002",
      "status": "pending",
      "progress": 0.0
    }
  ],
  "updated_at": "2025-01-15T10:40:00Z"
}
```

#### **GET** `/evaluations/jobs` \- List Evaluations

**Purpose**: List all evaluation requests with filtering capabilities **Response Model**: Array of `EvaluationResponse`

```shell
# List evaluations with filters
curl -X GET "{{baseUrl}}/evaluations/jobs?limit=10&status_filter=running"

# List all evaluations (default limit: 50, max: 100)
curl -X GET "{{baseUrl}}/evaluations/jobs"
```

#### **DELETE** `/evaluations/jobs/{id}` \- Cancel Evaluation

**Purpose**: Cancel a running evaluation

```shell
curl -X DELETE "{{baseUrl}}/evaluations/jobs/550e8400-e29b-41d4-a716-446655440000"
```

#### **GET** `/evaluations/jobs/{id}/summary` \- Get Evaluation Summary

**Purpose**: Get a summary of an evaluation request with detailed metrics **Response Model**: Generic object with evaluation summary

```shell
curl -X GET "{{baseUrl}}/evaluations/jobs/550e8400-e29b-41d4-a716-446655440000/summary"
```

**Response Example**:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "experiment_name": "comprehensive-model-assessment",
  "model": {
    "server": "vllm",
    "name": "meta-llama/llama-3.1-8b"
  },
  "total_duration_seconds": 1847.5,
  "completed_at": "2025-01-15T11:15:30Z",
  "benchmarks": [
    {
      "benchmark_id": "arc_easy",
      "provider_id": "lm_evaluation_harness",
      "evaluation_id": "eval_001",
      "status": "completed",
      "duration_seconds": 456.2,
      "metrics": {
        "accuracy": 0.8345,
        "acc_norm": 0.8421,
        "total_samples": 2376,
        "correct_answers": 1982,
        "stderr": 0.0087
      },
      "config": {
        "batch_size": 8,
        "num_fewshot": 0,
        "device": "cuda:0"
      }
    },
    {
      "benchmark_id": "hellaswag",
      "provider_id": "lm_evaluation_harness",
      "evaluation_id": "eval_001_batch",
      "status": "completed",
      "duration_seconds": 526.1,
      "batch_optimization": true,
      "metrics": {
        "accuracy": 0.7892,
        "acc_norm": 0.8156,
        "total_samples": 10042,
        "correct_answers": 7926,
        "stderr": 0.0041
      },
      "config": {
        "batch_size": 8,
        "num_fewshot": 10
      }
    },
    {
      "benchmark_id": "faithfulness",
      "provider_id": "ragas",
      "evaluation_id": "eval_002",
      "status": "completed",
      "duration_seconds": 287.4,
      "metrics": {
        "faithfulness_score": 0.8934,
        "total_samples": 500,
        "faithful_samples": 447,
        "mean_score": 0.8934,
        "std_score": 0.1245,
        "confidence_interval": [0.8823, 0.9045]
      },
      "config": {
        "dataset_path": "./data/rag_test_set.jsonl",
        "retrieval_system": "vector_db",
        "embeddings_model": "sentence-transformers/all-mpnet-base-v2"
      }
    },
    {
      "benchmark_id": "answer_relevancy",
      "provider_id": "ragas",
      "evaluation_id": "eval_003",
      "status": "completed",
      "duration_seconds": 312.6,
      "metrics": {
        "answer_relevancy_score": 0.7654,
        "total_samples": 500,
        "relevant_samples": 383,
        "mean_score": 0.7654,
        "std_score": 0.1876,
        "confidence_interval": [0.7489, 0.7819]
      },
      "config": {
        "dataset_path": "./data/rag_test_set.jsonl",
        "ground_truth_path": "./data/ground_truth.jsonl"
      }
    },
    {
      "benchmark_id": "prompt_injection",
      "provider_id": "garak",
      "evaluation_id": "eval_004",
      "status": "completed",
      "duration_seconds": 265.2,
      "metrics": {
        "injection_success_rate": 0.0450,
        "detection_rate": 0.9550,
        "total_attempts": 1000,
        "successful_injections": 45,
        "detected_injections": 955,
        "scan_modules": ["encoding", "leakage"],
        "confidence_score": 0.8723
      },
      "config": {
        "max_attempts": 100,
        "confidence_threshold": 0.8,
        "reporting_level": "detailed"
      }
    }
  ],
  "aggregate_metrics": {
    "total_benchmarks": 5,
    "completed_benchmarks": 5,
    "failed_benchmarks": 0,
    "overall_score": 0.7896,
    "provider_breakdown": {
      "lm_evaluation_harness": {
        "benchmarks": 2,
        "avg_accuracy": 0.8119,
        "total_samples": 12418
      },
      "ragas": {
        "benchmarks": 2,
        "avg_score": 0.8294,
        "total_samples": 1000
      },
      "garak": {
        "benchmarks": 1,
        "security_score": 0.9550,
        "total_attempts": 1000
      }
    }
  },
  "experiment_url": "http://mlflow:5000/experiments/12345",
  "artifacts": {
    "detailed_results": "s3://eval-results/550e8400-e29b-41d4-a716-446655440000/results.json",
    "logs": "s3://eval-results/550e8400-e29b-41d4-a716-446655440000/logs/",
    "mlflow_run_id": "a1b2c3d4e5f6"
  }
}
```

**Commentary**: Comprehensive evaluation summary showing detailed metrics for each benchmark across different providers. Includes provider-specific metrics (accuracy for LMEval, scores for RAGAS, security rates for Garak), execution details, and aggregated results with MLFlow integration.

---

### Provider Management Endpoints

#### **GET** `/evaluations/providers` \- List All Providers

**Purpose**: Discover available evaluation providers and their capabilities **Response Model**: `ListProvidersResponse`

```shell
curl -X GET "{{baseUrl}}/evaluations/providers"
```

**Response Example**:

```json
{
  "providers": [
    {
      "provider_id": "lm_evaluation_harness",
      "provider_name": "LM Evaluation Harness",
      "description": "Comprehensive evaluation framework for language models with 167 benchmarks",
      "provider_type": "builtin",
      "benchmark_count": 167,
      "categories": ["reasoning", "math", "science", "knowledge", "language_modeling"]
    },
    {
      "provider_id": "ragas",
      "provider_name": "RAGAS",
      "description": "Retrieval Augmented Generation Assessment framework",
      "provider_type": "builtin",
      "benchmark_count": 8,
      "categories": ["retrieval", "generation", "rag"]
    },
    {
      "provider_id": "garak",
      "provider_name": "Garak",
      "description": "LLM vulnerability scanner for security assessment",
      "provider_type": "builtin",
      "benchmark_count": 15,
      "categories": ["safety", "security", "robustness"]
    }
  ],
  "total_count": 3
}
```

**Commentary**: Provides provider discovery with capability summary. Each provider exposes different benchmark categories \- LM Eval Harness for comprehensive model evaluation, RAGAS for RAG-specific assessment, and Garak for security scanning.

#### **GET** `/evaluations/providers/{provider_id}` \- Get Provider Details

**Purpose**: Retrieve detailed provider information including all benchmarks

```shell
curl -X GET "{{baseUrl}}/evaluations/providers/lm_evaluation_harness"
```


---

### Benchmark Management Endpoints

#### **GET** `/evaluations/benchmarks` \- List All Benchmarks

**Purpose**: Discover available benchmarks across all providers with filtering **Response Model**: `ListBenchmarksResponse`

```shell
# List all benchmarks
curl -X GET "{{baseUrl}}/evaluations/benchmarks"

# Filter by provider
curl -X GET "{{baseUrl}}/evaluations/benchmarks?provider_id=lm_evaluation_harness"

# Filter by category
curl -X GET "{{baseUrl}}/evaluations/benchmarks?category=reasoning"

# Filter by tags
curl -X GET "{{baseUrl}}/evaluations/benchmarks?tags=math,science"
```

**Response Example**:

```json
{
  "benchmarks": [
    {
      "benchmark_id": "arc_easy",
      "provider_id": "lm_evaluation_harness",
      "name": "ARC Easy",
      "description": "AI2 Reasoning Challenge (Easy) - Grade school science questions",
      "category": "reasoning",
      "metrics": ["accuracy", "acc_norm"],
      "num_few_shot": 0,
      "dataset_size": 2376,
      "tags": ["reasoning", "science", "lm_eval"]
    },
    {
      "benchmark_id": "faithfulness",
      "provider_id": "ragas",
      "name": "Faithfulness",
      "description": "Measures factual consistency of generated answer against given context",
      "category": "retrieval",
      "metrics": ["faithfulness_score"],
      "num_few_shot": null,
      "dataset_size": null,
      "tags": ["rag", "faithfulness", "retrieval"]
    },
    {
      "benchmark_id": "prompt_injection",
      "provider_id": "garak",
      "name": "Prompt Injection",
      "description": "Tests model resistance to prompt injection attacks",
      "category": "safety",
      "metrics": ["injection_success_rate", "detection_rate"],
      "num_few_shot": 0,
      "dataset_size": 500,
      "tags": ["security", "safety", "injection"]
    }
  ],
  "total_count": 190,
  "providers_included": ["lm_evaluation_harness", "ragas", "garak"]
}
```

**Commentary**: Unified benchmark catalog across providers. Each benchmark has a clean `benchmark_id` and separate `provider_id` for clarity. Supports powerful filtering by provider, category, and tags for targeted discovery.


#### **GET** `/evaluations/providers/{provider_id}/benchmarks/{benchmark_id}` \- Get Benchmark Details

**Purpose**: Get details of a specific benchmark **Response Model**: `BenchmarkDetail`

```shell
curl -X GET "{{baseUrl}}/evaluations/providers/lm_evaluation_harness/benchmarks/arc_easy"
```

---

### Collection Management Endpoints

#### **GET** `/evaluations/collections` \- List Benchmark Collections

**Purpose**: Discover curated benchmark collections for specific domains
**Response Model**: `ListCollectionsResponse`

```shell
curl -X GET "{{baseUrl}}/evaluations/collections"
```

**Response Example**:

```json
{
  "collections": [
    {
      "collection_id": "healthcare_safety_v1",
      "name": "Healthcare Safety Assessment v1",
      "description": "Comprehensive safety evaluation for healthcare LLM applications",
      "benchmark_count": 12,
      "providers": ["lm_evaluation_harness", "garak"],
      "categories": ["safety", "medical", "reasoning"],
      "created_at": "2024-12-01T00:00:00Z"
    },
    {
      "collection_id": "general_llm_eval_v1",
      "name": "General LLM Evaluation v1",
      "description": "Standard evaluation suite for general-purpose language models",
      "benchmark_count": 25,
      "providers": ["lm_evaluation_harness"],
      "categories": ["reasoning", "knowledge", "math", "language_modeling"],
      "created_at": "2024-12-01T00:00:00Z"
    }
  ],
  "total_count": 4
}
```

**Commentary**: Pre-curated benchmark collections for domain-specific evaluation. Healthcare, automotive, finance, and general collections provide standardized evaluation suites for compliance and model validation.

#### **GET** `/evaluations/collections/{collection_id}` \- Get Collection Details

**Purpose**: Detailed collection specification with benchmark list and usage metrics

```shell
curl -X GET "{{baseUrl}}/evaluations/collections/healthcare_safety_v1"
```

**Response Example**:

```json
{
  "collection_id": "healthcare_safety_v1",
  "name": "Healthcare Safety Assessment v1",
  "description": "Comprehensive safety evaluation for healthcare LLM applications including medical knowledge, safety protocols, and regulatory compliance benchmarks",
  "version": "1.0",
  "created_at": "2024-12-01T00:00:00Z",
  "updated_at": "2025-01-10T14:30:00Z",
  "benchmark_count": 12,
  "categories": ["safety", "medical", "reasoning"],
  "providers": ["lm_evaluation_harness", "garak"],
  "benchmarks": [
    {
      "benchmark_id": "medqa",
      "provider_id": "lm_evaluation_harness",
      "name": "Medical Question Answering",
      "description": "Medical knowledge and reasoning assessment",
      "category": "medical",
      "weight": 0.2,
      "metrics": ["accuracy", "f1_score"],
      "required": true
    },
    {
      "benchmark_id": "medical_safety",
      "provider_id": "lm_evaluation_harness",
      "name": "Medical Safety Protocols",
      "description": "Healthcare safety and protocol compliance",
      "category": "safety",
      "weight": 0.3,
      "metrics": ["accuracy", "safety_score"],
      "required": true
    },
    {
      "benchmark_id": "hipaa_compliance",
      "provider_id": "garak",
      "name": "HIPAA Compliance Check",
      "description": "Privacy and regulatory compliance testing",
      "category": "safety",
      "weight": 0.25,
      "metrics": ["compliance_rate", "privacy_score"],
      "required": true
    },
    {
      "benchmark_id": "medical_reasoning",
      "provider_id": "lm_evaluation_harness",
      "name": "Clinical Reasoning",
      "description": "Medical diagnosis and treatment reasoning",
      "category": "reasoning",
      "weight": 0.25,
      "metrics": ["accuracy", "reasoning_score"],
      "required": false
    }
  ],
  "usage_statistics": {
    "total_evaluations": 247,
    "evaluations_30d": 45,
    "unique_models_tested": 23,
    "last_evaluation": "2025-01-15T09:30:00Z",
    "popular_models": [
      {
        "model_name": "meta-llama/llama-3.1-8b",
        "evaluation_count": 18,
        "avg_score": 0.8234
      },
      {
        "model_name": "microsoft/biogpt",
        "evaluation_count": 12,
        "avg_score": 0.7892
      }
    ]
  },
  "performance_metrics": {
    "avg_collection_duration_seconds": 2347.8,
    "success_rate": 0.9635,
    "avg_benchmark_scores": {
      "medqa": {
        "avg_accuracy": 0.7845,
        "evaluations_count": 45
      },
      "medical_safety": {
        "avg_accuracy": 0.8923,
        "avg_safety_score": 0.9156,
        "evaluations_count": 45
      },
      "hipaa_compliance": {
        "avg_compliance_rate": 0.9534,
        "avg_privacy_score": 0.9234,
        "evaluations_count": 43
      },
      "medical_reasoning": {
        "avg_accuracy": 0.7234,
        "avg_reasoning_score": 0.7456,
        "evaluations_count": 38
      }
    },
    "score_distribution": {
      "overall": {
        "mean": 0.8234,
        "std": 0.1456,
        "min": 0.5234,
        "max": 0.9534,
        "percentiles": {
          "p50": 0.8234,
          "p75": 0.8934,
          "p90": 0.9234,
          "p95": 0.9434
        }
      }
    }
  },
  "compliance_requirements": {
    "required_score_threshold": 0.75,
    "mandatory_benchmarks": ["medqa", "medical_safety", "hipaa_compliance"],
    "certification_level": "healthcare_grade",
    "regulatory_frameworks": ["HIPAA", "FDA_510k", "EU_MDR"]
  },
  "collection_tags": ["healthcare", "medical", "safety", "compliance", "regulatory"],
  "documentation_url": "https://docs.eval-hub.ai/evaluations/collections/healthcare_safety_v1",
  "citation": "Healthcare Safety Assessment Collection v1.0, TrustyAI Evaluation Hub, 2024"
}
```

**Commentary**: Comprehensive collection details including benchmark specifications with weights, usage statistics showing adoption patterns, performance metrics across all benchmarks, and compliance requirements for healthcare applications. Essential for understanding collection effectiveness and model certification requirements.

#### **POST** `/evaluations/collections` \- Create Custom Collection

**Purpose**: Create custom benchmark collections for organizational needs

```shell
curl -X POST "{{baseUrl}}/evaluations/collections" \
-H "Content-Type: application/json" \
-d '{
  "collection_id": "custom_eval_v1",
  "name": "Custom Evaluation Suite",
  "description": "Organization-specific benchmark collection",
  "benchmarks": [
    {
      "provider_id": "lm_evaluation_harness",
      "benchmark_id": "arc_easy"
    }
  ]
}'
```

#### **PUT** `/evaluations/collections/{collection_id}` \- Update Collection

#### **PATCH** `/evaluations/collections/{collection_id}` \- Patch Collection

Use PATCH to partially update a collection without resending the full payload.

#### **DELETE** `/evaluations/collections/{collection_id}` \- Delete Collection

---


### Monitoring & Metrics Endpoints

#### **GET** `/metrics` \- Prometheus Metrics

**Purpose**: Prometheus metrics endpoint for monitoring and observability

```shell
curl -X GET "{{baseUrl}}/metrics"
```

**Response Example** (Prometheus format):

```
# HELP eval_hub_active_evaluations Number of currently active evaluations
# TYPE eval_hub_active_evaluations gauge
eval_hub_active_evaluations 3

# HELP eval_hub_completed_evaluations_total Total number of completed evaluations
# TYPE eval_hub_completed_evaluations_total counter
eval_hub_completed_evaluations_total 1247

# HELP eval_hub_evaluation_duration_seconds Duration of evaluations in seconds
# TYPE eval_hub_evaluation_duration_seconds histogram
eval_hub_evaluation_duration_seconds_bucket{le="60"} 45
eval_hub_evaluation_duration_seconds_bucket{le="300"} 189
eval_hub_evaluation_duration_seconds_bucket{le="600"} 234
eval_hub_evaluation_duration_seconds_bucket{le="1800"} 267
eval_hub_evaluation_duration_seconds_bucket{le="+Inf"} 278
eval_hub_evaluation_duration_seconds_sum 68745.2
eval_hub_evaluation_duration_seconds_count 278

# HELP eval_hub_provider_evaluations_total Total evaluations by provider
# TYPE eval_hub_provider_evaluations_total counter
eval_hub_provider_evaluations_total{provider="lm_evaluation_harness"} 892
eval_hub_provider_evaluations_total{provider="ragas"} 234
eval_hub_provider_evaluations_total{provider="garak"} 121

# HELP eval_hub_system_cpu_usage_percent CPU usage percentage
# TYPE eval_hub_system_cpu_usage_percent gauge
eval_hub_system_cpu_usage_percent 65.2

# HELP eval_hub_system_memory_usage_percent Memory usage percentage
# TYPE eval_hub_system_memory_usage_percent gauge
eval_hub_system_memory_usage_percent 72.1

# HELP eval_hub_system_gpu_usage_percent GPU usage percentage
# TYPE eval_hub_system_gpu_usage_percent gauge
eval_hub_system_gpu_usage_percent 89.3

# HELP eval_hub_api_requests_total Total API requests
# TYPE eval_hub_api_requests_total counter
eval_hub_api_requests_total{method="GET",endpoint="/evaluations/jobs"} 1234
eval_hub_api_requests_total{method="POST",endpoint="/evaluations/jobs"} 456
eval_hub_api_requests_total{method="GET",endpoint="/evaluations/providers"} 789

# HELP eval_hub_api_request_duration_seconds API request duration
# TYPE eval_hub_api_request_duration_seconds histogram
eval_hub_api_request_duration_seconds_bucket{method="GET",endpoint="/evaluations/jobs",le="0.1"} 567
eval_hub_api_request_duration_seconds_bucket{method="GET",endpoint="/evaluations/jobs",le="0.5"} 1123
eval_hub_api_request_duration_seconds_bucket{method="GET",endpoint="/evaluations/jobs",le="1.0"} 1198
eval_hub_api_request_duration_seconds_bucket{method="GET",endpoint="/evaluations/jobs",le="+Inf"} 1234
```

**Commentary**: Standard Prometheus metrics format for integration with monitoring systems like Grafana, AlertManager, and Prometheus servers. Includes evaluation metrics, system resource usage, provider statistics, and API performance metrics.

#### **GET** `/metrics/system` \- Get System Metrics

**Purpose**: Get system metrics and statistics for monitoring and observability **Response Model**: Generic object with system metrics

```shell
curl -X GET "{{baseUrl}}/metrics/system"
```

**Response Example**:

```json
{
  "timestamp": "2025-01-15T11:30:00Z",
  "uptime_seconds": 2847392,
  "active_evaluations": 3,
  "completed_evaluations_24h": 45,
  "failed_evaluations_24h": 2,
  "average_evaluation_time_seconds": 245.5,
  "evaluation_queue": {
    "pending": 12,
    "running": 3,
    "size_limit": 100,
    "oldest_pending_age_seconds": 45
  },
  "system_resources": {
    "cpu_usage_percent": 65.2,
    "memory_usage_percent": 72.1,
    "disk_usage_percent": 45.8,
    "gpu_usage_percent": 89.3,
    "gpu_memory_usage_percent": 94.7,
    "network_io": {
      "bytes_sent_24h": 847392847,
      "bytes_received_24h": 924738291
    }
  },
  "providers": {
    "lm_evaluation_harness": {
      "status": "healthy",
      "evaluations_24h": 35,
      "success_rate_24h": 0.9714,
      "avg_duration_seconds": 412.3,
      "active_evaluations": 2,
      "last_successful_evaluation": "2025-01-15T11:15:30Z",
      "benchmarks_run_24h": {
        "arc_easy": 8,
        "hellaswag": 12,
        "mmlu": 15
      }
    },
    "ragas": {
      "status": "healthy",
      "evaluations_24h": 8,
      "success_rate_24h": 1.0000,
      "avg_duration_seconds": 298.7,
      "active_evaluations": 1,
      "last_successful_evaluation": "2025-01-15T11:25:15Z",
      "benchmarks_run_24h": {
        "faithfulness": 4,
        "answer_relevancy": 4
      }
    },
    "garak": {
      "status": "healthy",
      "evaluations_24h": 2,
      "success_rate_24h": 1.0000,
      "avg_duration_seconds": 187.4,
      "active_evaluations": 0,
      "last_successful_evaluation": "2025-01-15T10:45:22Z",
      "benchmarks_run_24h": {
        "prompt_injection": 2
      }
    }
  },
  "models": {
    "total_registered": 15,
    "active": 12,
    "model_servers": {
      "vllm": {
        "status": "healthy",
        "models_served": 8,
        "requests_24h": 1247,
        "avg_response_time_ms": 342.5
      },
      "ollama": {
        "status": "healthy",
        "models_served": 4,
        "requests_24h": 847,
        "avg_response_time_ms": 567.2
      }
    }
  },
  "storage": {
    "mlflow_experiments": 247,
    "total_artifact_size_gb": 156.7,
    "s3_storage_used_gb": 142.3,
    "local_cache_size_gb": 14.4
  },
  "performance_metrics": {
    "throughput_evaluations_per_hour": 12.4,
    "peak_concurrent_evaluations": 8,
    "api_requests_24h": 2847,
    "api_success_rate_24h": 0.9912,
    "avg_api_response_time_ms": 156.3
  },
  "health_checks": {
    "database": "healthy",
    "mlflow": "healthy",
    "s3_storage": "healthy",
    "redis_cache": "healthy",
    "model_servers": "healthy"
  }
}
```

**Commentary**: Comprehensive system metrics including evaluation queue status, resource utilization, provider-specific performance data, model server health, storage usage, and overall system performance indicators. Essential for monitoring service health and capacity planning.

---

## **Schema Design & Provider Parameters**

### ExperimentConfig Structure

The Evaluation Hub uses a structured `ExperimentConfig` object to organize MLFlow experiment tracking information. This replaces the previous scattered `experiment_name` and `tags` fields for better type safety and consistency.

**ExperimentConfig Schema**:
```json
{
  "experiment": {
    "name": "string",
    "tags": {
      "additionalProperties": "string"
    }
  }
}
```

**Benefits**:
- **Structured Organization**: Experiment configuration is clearly grouped
- **Type Safety**: Better OpenAPI validation and documentation
- **Consistency**: All evaluation endpoints use the same structure
- **MLFlow Integration**: Direct mapping to MLFlow experiment metadata


### Evaluation Request Structure

The `/evaluations` endpoint uses a simple, consistent structure for maximum clarity:

```
EvaluationRequest
├── model                           # Single model specification for all benchmarks
│   ├── server                      # Model server (vllm, ollama, etc.)
│   ├── name                        # Model identifier
│   └── configuration               # Model-specific params (temperature, etc.)
├── benchmarks[]                    # Array of benchmarks to run
│   ├── benchmark_id                # Direct benchmark identifier
│   ├── provider_id                 # Provider to use (lm_evaluation_harness, ragas, garak)
│   └── config                      # Provider-specific benchmark configuration
└── experiment                     # Experiment configuration for MLFlow
    ├── name                        # Required experiment name for MLFlow
    └── tags                        # Optional metadata tags
```

### Provider-Specific Parameter Examples

**LM-Evaluation-Harness Benchmarks:**

```json
{
  "benchmark_id": "mmlu",
  "provider_id": "lm_evaluation_harness",
  "config": {
    "num_fewshot": 5,                            // Few-shot examples
    "limit": null,                               // Sample limit (null = all)
    "batch_size": 16,                            // LMEval batch processing
    "use_cache": true,                           // Cache model outputs
    "trust_remote_code": false,                  // Security setting
    "write_out": true,                           // Write detailed outputs
    "device": "cuda:0",                          // GPU specification
    "dtype": "bfloat16",                         // Model precision
    "include_path": "./prompts/mmlu_cot.yaml",   // Custom prompts
    "fewshot_as_multiturn": false,               // LMEval specific
    "apply_chat_template": true                  // Chat model handling
  }
}
```

**RAGAS RAG Evaluation Benchmarks:**

```json
{
  "benchmark_id": "faithfulness",
  "provider_id": "ragas",
  "config": {
    "dataset_path": "./data/rag_test_set.jsonl",        // Custom dataset
    "ground_truth_path": "./data/ground_truth.jsonl",   // Reference answers
    "retrieval_system": "vector_db",                    // RAG system type
    "embeddings_model": "sentence-transformers/all-mpnet-base-v2",
    "chunk_size": 512,                                  // Document chunking
    "overlap": 50,                                      // Chunk overlap
    "top_k": 5,                                         // Retrieval top-k
    "batch_size": 4,                                    // RAGAS batch size
    "llm_temperature": 0.0,                             // LLM for evaluation
    "metrics": ["faithfulness"]                         // Specific metrics to compute
  }
}
```

**Garak Security Scanning Benchmarks:**

```json
{
  "benchmark_id": "prompt_injection",
  "provider_id": "garak",
  "config": {
    "max_attempts": 100,                               // Attempts per probe
    "confidence_threshold": 0.8,                       // Success threshold
    "scan_modules": ["encoding", "leakage"],           // Security domains
    "reporting_level": "detailed",                     // Output verbosity
    "parallel_requests": 10,                           // Concurrency
    "timeout": 30,                                     // Request timeout
    "user_agent": "eval-hub/garak-scanner",            // Custom UA
    "custom_probes_path": "./probes/",                 // Custom probe definitions
    "exclude_patterns": ["test_*"]                     // Skip test probes
  }
}
```

### Configuration Patterns

The simplified structure provides clear configuration patterns:

1. **Model Configuration**: Single model applies to all benchmarks in the request
2. **Benchmark Configuration**: Each benchmark has its own provider-specific config
3. **Provider Consistency**: All config for a provider goes in the benchmark's `config` object
4. **Flat Organization**: No artificial grouping \- just a direct list of benchmarks to execute

**Example of Mixed Providers in Single Request:**

```json
{
  "model": {
    "server": "vllm",
    "name": "meta-llama/llama-3.1-8b",
    "configuration": {
      "temperature": 0.1,           // Applies to all benchmarks
      "max_tokens": 512
    }
  },
  "benchmarks": [
    {
      "benchmark_id": "arc_easy",
      "provider_id": "lm_evaluation_harness",
      "config": {
        "batch_size": 32,           // Specific to this benchmark
        "num_fewshot": 0,
        "device": "cuda:0"
      }
    },
    {
      "benchmark_id": "mmlu",
      "provider_id": "lm_evaluation_harness",
      "config": {
        "batch_size": 16,           // Different batch size for this benchmark
        "num_fewshot": 5,
        "device": "cuda:0"
      }
    },
    {
      "benchmark_id": "faithfulness",
      "provider_id": "ragas",
      "config": {
        "dataset_path": "./data/rag_test_set.jsonl",
        "batch_size": 4             // RAGAS-specific configuration
      }
    },
    {
      "benchmark_id": "prompt_injection",
      "provider_id": "garak",
      "config": {
        "max_attempts": 100,        // Garak-specific configuration
        "confidence_threshold": 0.8
      }
    }
  ],
  "experiment": {
    "name": "comprehensive-assessment",
    "tags": {
      "model_version": "v2.1.0",
      "evaluation_type": "comprehensive"
    }
  }
}
```

---

## **Advanced Features**

### Asynchronous Execution Pattern

The API supports both synchronous and asynchronous execution:

- **Async Mode** (default): Returns immediately with tracking ID, enables parallel processing
- **Sync Mode**: Blocks until completion, suitable for simple workflows
- **Status Polling**: Regular status checks for async evaluations
- **Callback URLs**: Optional webhook notifications on completion

### Batch Processing Capabilities

```shell
# Submit multiple benchmarks in single request
curl -X POST "{{baseUrl}}/evaluations" \
-d '{
  "model": {
    "server": "vllm",
    "name": "meta-llama/llama-3.1-8b"
  },
  "benchmarks": [
    {
      "benchmark_id": "arc_easy",
      "provider_id": "lm_evaluation_harness",
      "config": {"num_fewshot": 0}
    },
    {
      "benchmark_id": "faithfulness",
      "provider_id": "ragas",
      "config": {"dataset_path": "./data/test.jsonl"}
    },
    {
      "benchmark_id": "prompt_injection",
      "provider_id": "garak",
      "config": {"max_attempts": 100}
    }
  ]
}'
```

### MLFlow Integration

- **Experiment Tracking**: Automatic MLFlow experiment creation
- **Result Persistence**: Metrics and artifacts stored in MLFlow
- **Lineage Tracking**: Full evaluation provenance
- **Model Registry**: Integration with model governance workflows

### Error Handling & Validation

- **Request Validation**: Comprehensive Pydantic-based validation
- **Provider Validation**: Backend capability verification
- **Graceful Degradation**: Partial execution on provider failures
- **Detailed Error Messages**: Structured error responses with context

---

## **Integration Patterns**

### Enterprise MLOps Workflow

1. **Model Registration**: Register models via `/models` endpoint
2. **Collection Selection**: Choose domain-specific collections
3. **Evaluation Submission**: Submit batch evaluations
4. **Progress Monitoring**: Poll status endpoints
5. **Result Retrieval**: Access results via MLFlow integration
6. **Governance Integration**: Surface metrics in model registry

### Development & Testing Workflow

1. **Health Check**: Verify service status
2. **Provider Discovery**: List available providers and benchmarks
3. **Single Evaluation**: Test with `/evaluations/single`
4. **Batch Evaluation**: Scale to multiple benchmarks
5. **Custom Collections**: Create organization-specific suites

### Continuous Integration Pattern

```shell
# CI/CD Pipeline Integration
./evaluate-model.sh model-v2.1 healthcare_safety_v1
# 1. Register model endpoint
# 2. Submit collection evaluation
# 3. Wait for completion
# 4. Parse results for gate decisions
```

---

## **Collection Evaluation Flow**

The eval-hub API efficiently handles collection evaluation by destructuring collections into individual benchmarks and routing them to appropriate providers. This section shows how the API optimizes provider execution through intelligent batching.

### Collection to Provider Routing

```
sequenceDiagram
    participant Client
    participant API as Eval-Hub API
    participant DB as Collection DB
    participant LMEval as LM-Eval Provider
    participant RAGAS as RAGAS Provider
    participant Garak as Garak Provider
    participant MLFlow as MLFlow Tracking

    Note over Client,MLFlow: Collection Evaluation Request Flow

    Client->>API: POST /evaluations/collections/healthcare_safety_v1
    Note right of Client: Collection-based evaluation request

    API->>DB: GET /evaluations/collections/healthcare_safety_v1
    DB->>API: Collection details with benchmark list

    Note over API: Collection contains:<br/>- medqa (lm_evaluation_harness)<br/>- medical_safety (lm_evaluation_harness)<br/>- medical_reasoning (lm_evaluation_harness)<br/>- hipaa_compliance (garak)<br/>- faithfulness (ragas)<br/>- answer_relevancy (ragas)

    Note over API: Group benchmarks by provider

    rect rgb(200, 230, 255)
        Note over API,LMEval: LM-Eval supports batch execution
        API->>LMEval: Single request with multiple benchmarks
        Note right of API: POST /evaluate<br/>{<br/>  "model": {...},<br/>  "benchmarks": [<br/>    "medqa",<br/>    "medical_safety",<br/>    "medical_reasoning"<br/>  ]<br/>}
        LMEval->>LMEval: Execute all 3 benchmarks in single run
        LMEval->>API: Batch results for all 3 benchmarks
    end

    rect rgb(255, 230, 200)
        Note over API,RAGAS: RAGAS individual executions
        API->>RAGAS: POST /evaluate (faithfulness)
        API->>RAGAS: POST /evaluate (answer_relevancy)
        RAGAS->>API: faithfulness results
        RAGAS->>API: answer_relevancy results
    end

    rect rgb(230, 255, 200)
        Note over API,Garak: Garak individual execution
        API->>Garak: POST /evaluate (hipaa_compliance)
        Garak->>API: hipaa_compliance results
    end

    Note over API: Aggregate all provider results
    API->>MLFlow: Log collection evaluation experiment
    MLFlow->>API: Experiment ID and tracking URL

    API->>Client: 202 ACCEPTED<br/>{<br/>  "id": "uuid",<br/>  "collection_id": "healthcare_safety_v1",<br/>  "evaluations": [<br/>    {"benchmark_id": "medqa", "provider_id": "lm_evaluation_harness"},<br/>    {"benchmark_id": "medical_safety", "provider_id": "lm_evaluation_harness"},<br/>    {"benchmark_id": "medical_reasoning", "provider_id": "lm_evaluation_harness"},<br/>    {"benchmark_id": "hipaa_compliance", "provider_id": "garak"},<br/>    {"benchmark_id": "faithfulness", "provider_id": "ragas"},<br/>    {"benchmark_id": "answer_relevancy", "provider_id": "ragas"}<br/>  ]<br/>}

    Note over Client,MLFlow: Parallel execution across providers<br/>LMEval: 1 batch job (3 benchmarks)<br/>RAGAS: 2 individual jobs<br/>Garak: 1 individual job
```

### API Implementation Flow

```
flowchart TD
    A[Collection Evaluation Request] --> B[Fetch Collection Definition]
    B --> C[Extract Benchmark List]
    C --> D[Group by Provider]

    D --> E{Provider Capabilities}

    E -->|LM-Eval<br/>Batch Capable| F[Create Single LMEval Request<br/>Multiple Benchmarks]
    E -->|RAGAS<br/>Individual Only| G[Create Multiple RAGAS Requests<br/>One per Benchmark]
    E -->|Garak<br/>Individual Only| H[Create Multiple Garak Requests<br/>One per Benchmark]

    F --> I[Execute LMEval Batch]
    G --> J[Execute RAGAS Jobs]
    H --> K[Execute Garak Jobs]

    I --> L[Collect Results]
    J --> L
    K --> L

    L --> M[Aggregate Collection Results]
    M --> N[Log to MLFlow]
    N --> O[Return Tracking Response]

    style F fill:#e1f5fe
    style G fill:#fff3e0
    style H fill:#f3e5f5
```

#### **POST** `/evaluations/collections/{collection_id}` \- Evaluate Collection

**Purpose**: Submit evaluation for an entire benchmark collection with automatic provider optimization **Response Model**: `EvaluationResponse` **Status Code**: `202 ACCEPTED`

```shell
curl -X POST "{{baseUrl}}/evaluations/collections/healthcare_safety_v1?async_mode=true" \
-H "Content-Type: application/json" \
-d '{
  "model": {
    "server": "vllm",
    "name": "meta-llama/llama-3.1-8b",
    "configuration": {
      "temperature": 0.1,
      "max_tokens": 512,
      "top_p": 0.95
    }
  },
  "experiment_name": "healthcare-model-certification",
  "tags": {
    "environment": "production",
    "certification": "healthcare",
    "model_version": "v2.1.0"
  }
}'
```

**Response Example**:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "pending",
  "collection_id": "healthcare_safety_v1",
  "collection_name": "Healthcare Safety Assessment v1",
  "optimizations_applied": {
    "lm_evaluation_harness": {
      "benchmarks_batched": 3,
      "original_jobs": 3,
      "optimized_jobs": 1,
      "time_savings_percent": 67
    },
    "ragas": {
      "benchmarks_batched": 0,
      "original_jobs": 2,
      "optimized_jobs": 2,
      "time_savings_percent": 0
    },
    "garak": {
      "benchmarks_batched": 0,
      "original_jobs": 1,
      "optimized_jobs": 1,
      "time_savings_percent": 0
    }
  },
  "benchmarks": [
    {
      "benchmark_id": "medqa",
      "provider_id": "lm_evaluation_harness",
      "evaluation_id": "eval_001_batch",
      "status": "pending",
      "batch_optimization": true,
      "created_at": "2025-01-15T10:30:00Z"
    },
    {
      "benchmark_id": "medical_safety",
      "provider_id": "lm_evaluation_harness",
      "evaluation_id": "eval_001_batch",
      "status": "pending",
      "batch_optimization": true,
      "created_at": "2025-01-15T10:30:00Z"
    },
    {
      "benchmark_id": "medical_reasoning",
      "provider_id": "lm_evaluation_harness",
      "evaluation_id": "eval_001_batch",
      "status": "pending",
      "batch_optimization": true,
      "created_at": "2025-01-15T10:30:00Z"
    },
    {
      "benchmark_id": "faithfulness",
      "provider_id": "ragas",
      "evaluation_id": "eval_002",
      "status": "pending",
      "created_at": "2025-01-15T10:30:00Z"
    },
    {
      "benchmark_id": "answer_relevancy",
      "provider_id": "ragas",
      "evaluation_id": "eval_003",
      "status": "pending",
      "created_at": "2025-01-15T10:30:00Z"
    },
    {
      "benchmark_id": "hipaa_compliance",
      "provider_id": "garak",
      "evaluation_id": "eval_004",
      "status": "pending",
      "created_at": "2025-01-15T10:30:00Z"
    }
  ],
  "total_jobs": 4,
  "total_benchmarks": 6,
  "estimated_completion": "2025-01-15T11:45:00Z",
  "experiment_id": "exp_12345",
  "experiment_url": "http://mlflow:5000/experiments/12345",
  "created_at": "2025-01-15T10:30:00Z"
}
```

**Commentary**: Collection evaluation automatically optimizes execution by:

1. **Provider Grouping**: Benchmarks are grouped by provider\_id
2. **Batch Optimization**: LM-Evaluation-Harness supports multi-benchmark execution, reducing from 3 jobs to 1
3. **Parallel Execution**: Different providers run concurrently (4 total jobs vs 6 sequential)
4. **Time Efficiency**: \~60% reduction in total execution time through intelligent batching
5. **Resource Optimization**: Fewer job spawns, better resource utilization
6. **Transparent Tracking**: Full visibility into optimization decisions and job structure

---

## **API Examples and Use Cases**
