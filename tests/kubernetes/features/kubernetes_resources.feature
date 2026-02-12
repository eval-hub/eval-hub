Feature: Kubernetes Resources Validation
  As a platform engineer
  I want to ensure Kubernetes resources are created correctly
  So that evaluation jobs run reliably in a Kubernetes cluster

  Background:
    Given the service is running with Kubernetes runtime

  Scenario: Job and ConfigMap specification (multi-benchmark)
    Given the provider has a multiline entrypoint command
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_multi_benchmark.json"
    And the job has 3 benchmarks configured
    Then the response code should be 202
    And the response should be returned immediately without waiting for Job creation
    And Jobs should be created in the background
    And the number of Jobs created should equal the number of benchmarks
    And the number of ConfigMaps created should equal the number of benchmarks
    And each Job should have a unique benchmark_id label
    And each ConfigMap should have a unique benchmark_id label
    And a ConfigMap should be created with name pattern "eval-job-{id}-{benchmark_id}-spec"
    And the ConfigMap should have label "app" with value "evalhub"
    And the ConfigMap should have label "component" with value "evaluation-job"
    And the ConfigMap should have label "job_id" matching the evaluation job ID
    And the ConfigMap should have label "provider_id" matching the provider ID
    And the ConfigMap should have label "benchmark_id" matching the benchmark ID
    And the ConfigMap should contain data key "job.json"
    And the ConfigMap data "job.json" should be valid JSON
    And the ConfigMap data "job.json" should contain field "id" with the job ID
    And the ConfigMap data "job.json" should contain field "benchmark_id"
    And the ConfigMap data "job.json" should contain field "model.url"
    And the ConfigMap data "job.json" should contain field "model.name"
    And the ConfigMap data "job.json" should contain field "callback_url"
    And the ConfigMap data "job.json" should contain field "benchmark_config" as object
    And a Kubernetes Job should be created with name pattern "eval-job-{id}-{benchmark_id}"
    And the Job should have label "app" with value "evalhub"
    And the Job should have label "component" with value "evaluation-job"
    And the Job should have label "job_id" matching the evaluation job ID
    And the Job should have label "provider_id" matching the provider ID
    And the Job should have label "benchmark_id" matching the benchmark ID
    And the Job spec should have "backoffLimit" set to the configured retry attempts
    And the Job spec should have "ttlSecondsAfterFinished" set to 3600
    And the Job spec template should have "restartPolicy" set to "Never"
    And the Job name should be lowercase
    And the Job name should not exceed 63 characters
    And the Job name should only contain alphanumeric characters and hyphens
    And the Job name should not start or end with a hyphen
    And the Job pod template should have container named "adapter"
    And the container should use the provider's configured image
    And the container should have "imagePullPolicy" set to "Always"
    And the container should have environment variable "JOB_ID" set to the job ID
    And the container command should be a valid array
    And the container command should not contain empty strings
    And the container command should have trimmed whitespace from each element
    And the container securityContext should have "allowPrivilegeEscalation" set to false
    And the container securityContext should have "runAsNonRoot" set to true
    And the container securityContext capabilities should drop "ALL"
    And the container securityContext should have seccompProfile type "RuntimeDefault"
    And the container should have CPU request set to provider's configured value or default "250m"
    And the container should have memory request set to provider's configured value or default "512Mi"
    And the container should have CPU limit set to provider's configured value or default "1"
    And the container should have memory limit set to provider's configured value or default "2Gi"
    And the Job pod template should have volume "job-spec" of type ConfigMap
    And the Job pod template should have volume "data" of type EmptyDir
    And the volume "job-spec" should reference the ConfigMap with suffix "-spec"
    And the container should have volumeMount "job-spec" at path "/meta/job.json"
    And the container should have volumeMount "data" at path "/data"
    And the volumeMount "job-spec" should have subPath "job.json"
    And the volumeMount "job-spec" should be readOnly
    And the container should have environment variables from the provider configuration
    And the environment variable "JOB_ID" should not be overridden by provider variables
    And for benchmark "mmlu" the ConfigMap data "job.json" should contain field "num_examples" with value from parameters
    And for benchmark "hellaswag" the ConfigMap data "job.json" field "benchmark_config" should not contain "num_examples"
    And for benchmark "hellaswag" the ConfigMap data "job.json" should contain field "benchmark_config" as empty object
    And the ConfigMap should have an ownerReference of kind "Job"
    And the ConfigMap ownerReference should have controller set to true
    And the ConfigMap ownerReference should reference the created Job
    And the Job pod template should have serviceAccountName set to "evalhub-jobs"
    And the container should have environment variable "EVALHUB_URL" with pattern "https://evalhub.{namespace}.svc.cluster.local:8443"
    And the Job pod template should have volume "evalhub-service-ca" of type ConfigMap
    And the volume "evalhub-service-ca" should reference ConfigMap "evalhub-service-ca"
    And the container should have volumeMount "evalhub-service-ca" at path "/etc/pki/ca-trust/source/anchors"
    And the volumeMount "evalhub-service-ca" should be readOnly
    And at most 5 benchmarks should be processed concurrently

  Scenario: Delete evaluation job behaviors
    Given I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_basic.json"
    And the response code should be 202
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    And all Jobs associated with the evaluation job should be deleted
    And all ConfigMaps associated with the evaluation job should be deleted
    And the Job deletion should use propagationPolicy "Background"
    Given I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_basic.json"
    And the response code should be 202
    And the Job has already been deleted manually
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    And no error should be returned for NotFound resources
    Given I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_basic.json"
    And the response code should be 202
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=false"
    Then the response code should be 204
    And the Jobs should still exist in Kubernetes
    And the ConfigMaps should still exist in Kubernetes
    Given I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_multi_benchmark.json"
    And the response code should be 202
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    And DeleteEvaluationJobResources should be called
    And all 3 Jobs should be deleted from Kubernetes
    And all 3 ConfigMaps should be deleted from Kubernetes
    
  Scenario: Validation and configuration error responses
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_missing_model.json"
    Then the response code should be 400
    And the response should contain error about missing model information

  Scenario: MLflow fields are included in job spec
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_with_experiment.json"
    Then the response code should be 202
    And the ConfigMap data "job.json" should contain field "experiment_name"
    And the ConfigMap data "job.json" should contain field "tags" as array
