local test = import 'test.libsonnet';

// Commit on this branch that introduced tests/git-testdata (arc_easy + tokenizer).
// Override with TEST_DATA_GIT_SHA_REF when cloning a different history (e.g. after squash).
local defaultGitShaRef = '24714484d1bbd2047043d053a68a8c2f21579e3f';

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-git-sha',
    benchmarks: [
      test.gitArcEasyBenchmark({}, {
        // Hex commit SHA only — do not fall back to a branch name (TEST_DATA_GIT_REF/main).
        // Optionally override URL with TEST_DATA_GIT_SHA_URL (else TEST_DATA_GIT_URL).
        url: test.env('TEST_DATA_GIT_SHA_URL', test.env('TEST_DATA_GIT_URL', 'https://github.com/eval-hub/eval-hub')),
        ref: test.env('TEST_DATA_GIT_SHA_REF', defaultGitShaRef),
      }),
    ],
    tags: ['environment', 'git', 'sha'],
  },
  test.experiment('my-test-experiment'),
)
