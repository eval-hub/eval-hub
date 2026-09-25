local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job',
    } + if collectionId == '' then {
      benchmarks: [
        test.defaultBenchmark() + {
          test_data_ref: {
            hf: {
              repo_id: test.env(
                'HF_GATED_REPO_ID',
                'gated_repo',
              ),
              revision: test.env('HF_TEST_REVISION', 'main'),
              sub_path: test.env('HF_TEST_SUB_PATH', 'data'),
              secret_ref: test.env(
                'HF_SECRET_REF',
                'hftoken',
              ),
            },
          },
        },
      ],
      tags: ['environment'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment'),
)
