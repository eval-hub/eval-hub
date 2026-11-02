package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/internal/safefile"
	"github.com/eval-hub/eval-hub/pkg/api"
	"k8s.io/apimachinery/pkg/util/validation"
)

// downloader holds configuration for one source. Credentials and destinations
// are explicit, so successive downloads cannot inherit another source's state.
type downloader struct {
	source                                       api.TestDataRef
	destDir, scrtDir, gitMetadataDir, hfCacheDir string
}

// Preserve the existing evaluation init environment interface.
func downloaderFromEnv() downloader {
	return downloader{
		source: api.TestDataRef{
			S3:  &api.S3TestDataRef{Bucket: os.Getenv(envBucket), Key: os.Getenv(envKey)},
			Git: &api.GitTestDataRef{URL: os.Getenv(envGitURL), Ref: os.Getenv(envGitRef), SubPath: os.Getenv(envGitSubPath)},
			HF:  &api.HFTestDataRef{RepoID: os.Getenv(envHFRepoID), Revision: os.Getenv(envHFRevision), SubPath: os.Getenv(envHFSubPath)},
		},
		destDir: destDir, scrtDir: scrtDir, gitMetadataDir: gitMetadataDir, hfCacheDir: hfCacheDir,
	}
}

func runJobSources(path, root, secretRoot string) error {
	data, err := safefile.ReadFile(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return fmt.Errorf("read init job: %w", err)
	}
	var spec shared.JobSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return fmt.Errorf("decode init job: %w", err)
	}
	refs, err := shared.PostProcessingDataSources(spec.Parameters)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		// PVCs are mounted by Kubernetes. MLflow/OCI are resolved by the adapter
		// through the sidecar and do not appear in this list.
		if ref.Source.PVC != nil {
			continue
		}
		d, err := sourceDownloader(ref, root, secretRoot)
		if err != nil {
			return err
		}
		switch {
		case ref.Source.S3 != nil:
			err = d.runS3()
		case ref.Source.Git != nil:
			err = d.runGit()
		case ref.Source.HF != nil:
			err = d.runHF()
		}
		if err != nil {
			return fmt.Errorf("download %s: %w", ref.Name, err)
		}
	}
	return nil
}

func sourceDownloader(ref shared.NamedDataSource, root, secretRoot string) (downloader, error) {
	secret := ""
	switch {
	case ref.Source.S3 != nil:
		secret = ref.Source.S3.SecretRef
		if strings.TrimSpace(secret) == "" {
			return downloader{}, fmt.Errorf("%s: S3 secret_ref is required", ref.Name)
		}
	case ref.Source.Git != nil:
		secret = ref.Source.Git.SecretRef
	case ref.Source.HF != nil:
		secret = ref.Source.HF.SecretRef
	}
	if secret != "" && len(validation.IsDNS1123Subdomain(secret)) != 0 {
		return downloader{}, fmt.Errorf("%s: invalid secret_ref", ref.Name)
	}
	metadata := filepath.Join(root, ".metadata", ref.Name)
	// Public sources must not inherit credentials from another reference.
	secretDir := filepath.Join(root, ".public", ref.Name)
	if secret != "" {
		secretDir = filepath.Join(secretRoot, secret)
	}
	return downloader{source: ref.Source, destDir: filepath.Join(root, ref.Name), scrtDir: secretDir, gitMetadataDir: metadata, hfCacheDir: filepath.Join(metadata, "hf-cache")}, nil
}
