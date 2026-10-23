package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/internal/safefile"
)

const (
	envInitDestination   = "EVALHUB_INIT_DESTINATION"
	envInitSecretDir     = "EVALHUB_INIT_SECRET_DIR"
	envInitMetadataDir   = "EVALHUB_INIT_METADATA_DIR"
	envStagingRoot       = "EVALHUB_DATA_STAGING_ROOT"
	envStagingSecretRoot = "EVALHUB_DATA_STAGING_SECRET_ROOT"
)

var stagingName = regexp.MustCompile(`^(results|calibration-[0-9]+)$`)

func runStagingPlan(path string) error {
	data, err := safefile.ReadFile(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return fmt.Errorf("read data staging plan: %w", err)
	}
	var plan shared.DataStagingPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return fmt.Errorf("decode data staging plan: %w", err)
	}
	return stageDataSources(plan, executeStagedSource)
}

// Workers are separate invocations of the same init executable. That reuses the
// evaluation downloaders unchanged without sharing mutable env/path configuration
// between different references or leaking one source's credentials to another.
func stageDataSources(plan shared.DataStagingPlan, execute func(shared.DataStagingSource) error) error {
	if len(plan.Sources) == 0 {
		return fmt.Errorf("data staging plan must contain at least one source")
	}
	seen := map[string]bool{}
	for _, source := range plan.Sources {
		if !stagingName.MatchString(source.Name) || seen[source.Name] {
			return fmt.Errorf("invalid or duplicate staging source name %q", source.Name)
		}
		seen[source.Name] = true
		if _, err := stagingSourceEnv(source); err != nil {
			return err
		}
	}
	for _, source := range plan.Sources {
		if err := execute(source); err != nil {
			return fmt.Errorf("stage %s: %w", source.Name, err)
		}
	}
	return nil
}

func stagingSourceEnv(source shared.DataStagingSource) (map[string]string, error) {
	root := os.Getenv(envStagingRoot)
	if root == "" {
		root = shared.PostProcessorStagingRoot
	}
	secretRoot := os.Getenv(envStagingSecretRoot)
	if secretRoot == "" {
		secretRoot = shared.PostProcessorSecretRoot
	}
	if !filepath.IsAbs(root) || !filepath.IsAbs(secretRoot) {
		return nil, fmt.Errorf("staging roots must be absolute paths")
	}
	values := map[string]string{
		shared.DataStagingPlanEnv: "",
		envHFRepoID:               "", envHFRevision: "", envHFSubPath: "",
		envGitURL: "", envGitRef: "", envGitSubPath: "",
		envBucket: "", envKey: "",
		envInitDestination: filepath.Join(root, source.Name),
		envInitMetadataDir: filepath.Join(root, ".metadata", source.Name),
	}
	count := 0
	secret := ""
	if s3 := source.Source.S3; s3 != nil {
		count++
		secret = s3.SecretRef
		values[envBucket], values[envKey] = s3.Bucket, s3.Key
		if strings.TrimSpace(secret) == "" {
			return nil, fmt.Errorf("%s: S3 secret_ref is required", source.Name)
		}
	}
	if git := source.Source.Git; git != nil {
		count++
		secret = git.SecretRef
		values[envGitURL], values[envGitRef], values[envGitSubPath] = git.URL, git.Ref, git.SubPath
	}
	if hf := source.Source.HF; hf != nil {
		count++
		secret = hf.SecretRef
		values[envHFRepoID], values[envHFRevision], values[envHFSubPath] = hf.RepoID, hf.Revision, hf.SubPath
	}
	if count != 1 || source.Source.PVC != nil {
		return nil, fmt.Errorf("%s: staging requires exactly one S3, Git, or HF source", source.Name)
	}
	secret = strings.TrimSpace(secret)
	if secret != "" && (secret == "." || secret == ".." || filepath.Base(secret) != secret || strings.ContainsAny(secret, `/\`)) {
		return nil, fmt.Errorf("%s: invalid secret_ref", source.Name)
	}
	// Public repositories must not inherit credentials from another source.
	if secret == "" {
		values[envInitSecretDir] = filepath.Join(root, ".public", source.Name)
	} else {
		values[envInitSecretDir] = filepath.Join(secretRoot, secret)
	}
	return values, nil
}

func executeStagedSource(source shared.DataStagingSource) error {
	values, err := stagingSourceEnv(source)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(executable)
	for _, env := range os.Environ() {
		key, _, _ := strings.Cut(env, "=")
		if _, overridden := values[key]; !overridden {
			cmd.Env = append(cmd.Env, env)
		}
	}
	for key, value := range values {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
