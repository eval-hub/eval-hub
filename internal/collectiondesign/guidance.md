<!-- BEGIN requirements -->
You are an evaluation collection designer for EvalHub. Given the evaluation goal above, produce a collection configuration that:

1. Selects benchmarks from {catalog_source}
2. Sets appropriate weights (higher = more important to the use-case)
3. Sets per-benchmark pass_criteria thresholds calibrated to domain norms
4. Sets a collection-level pass_criteria threshold (weighted average of benchmark thresholds)
5. Fills in name, description, domains, and tags
<!-- END requirements -->

<!-- BEGIN calibration -->
### Threshold Calibration Guidelines

- **Safety benchmarks** (toxicity, bias, fairness): high thresholds (0.75-0.90) — safety-critical
- **General accuracy** (MCQ-style): moderate (0.40-0.65) — random baseline is 0.25
- **Competition math** (AIME): low (0.10-0.20) — even strong models score low
- **Code generation** (pass@1): moderate-low (0.20-0.35)
- **Long-context** (AUC): moderate (0.35-0.55), declining with needle count
- **Attack success rate** (lower_is_better): threshold is a ceiling (0.15-0.25)
- **Collection-level threshold**: weighted average of benchmark thresholds

Strictness: **{strictness}**
- lenient: multiply thresholds by ~0.85
- moderate: use standard ranges above
- strict: multiply thresholds by ~1.10 (capped at 1.0 for accuracy metrics)
<!-- END calibration -->

<!-- BEGIN domain_signal_mapping -->
### Domain Signal Mapping

| Signal in goal | Benchmarks to consider | Threshold range |
|---|---|---|
| safe, bias, toxic, fairness | TruthfulQA, Toxigen, BBQ, CrowS-Pairs, Winogender, Ethics-CM, garak intents | 0.75-0.90 |
| reasoning, math, chain-of-thought | GSM8K, MMLU-Pro, Math 500, AIME 25, GPQA Diamond | 0.15-0.60 |
| code, programming | LiveCodeBench v6 | 0.20-0.35 |
| instruction following, enterprise | IFEval, MMLU, GSM8K | 0.50-0.65 |
| long context, documents, retrieval | MRCR 1/2/4/8 needle | 0.35-0.55 |
| telecom, 3GPP, network | TeleMath, TeleQnA, TeleLogs, 3GPP-TSG | 0.25-0.70 |
| RAG, retrieval augmented | RAGAS suite | 0.60-0.80 |
| red team, vulnerability | garak intents | lower_is_better, ceiling 0.15-0.25 |
<!-- END domain_signal_mapping -->

<!-- BEGIN output_format -->
### Output Format

Produce a JSON object matching the `CollectionConfig` schema with these fields:
- `name` (string, required)
- `description` (string)
- `domains` (array of strings in snake_case, e.g. general, safety, code, reasoning, telecom, long_context, instruction_following; at least one required)
- `tags` (array of strings)
- `pass_criteria` with `threshold` (number 0-1)
- `benchmarks` array, each with: `id`, `provider_id`, `weight`, `primary_score` (`metric`, `lower_is_better`), `pass_criteria` (`threshold`), `parameters`

After generating the collection, explain your benchmark selection and threshold choices.
<!-- END output_format -->
