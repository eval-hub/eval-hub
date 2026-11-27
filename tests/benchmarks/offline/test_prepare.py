import io
import json
from pathlib import Path
import unittest
import zipfile

from prepare import bundle_name, read_archive


class ArchiveTests(unittest.TestCase):
    def archive(self, files):
        data = io.BytesIO()
        with zipfile.ZipFile(data, "w") as zipped:
            for name, body in files.items():
                zipped.writestr(name, body)
        data.seek(0)
        return data

    def test_missing_columns_fail_before_export(self):
        with self.assertRaisesRegex(ValueError, "Missing records/columns"):
            read_archive({"subset": "qasper", "splits": ["validation"]},
                         self.archive({"qasper/validation.jsonl": '{"text": "wrong dataset"}\n'}), 1)

    def test_qasper_preserves_raw_output_for_task_preprocessing(self):
        row = {"id": "question", "pid": "paper", "input": "问题\n\npaper", "output": "answer"}
        data = self.archive({"qasper/validation.jsonl": (json.dumps(row, ensure_ascii=False) + "\n") * 2})
        result = read_archive({"subset": "qasper", "splits": ["validation"]}, data, 1)
        self.assertEqual(result["validation"], [row])

    def test_manifest_matches_staged_job_definitions(self):
        root = Path(__file__).resolve().parents[1]
        specs = json.loads((root / "offline/datasets.json").read_text())
        self.assertEqual(len({s["benchmark"] for s in specs}), 7)
        for spec in specs:
            with self.subTest(benchmark=spec["benchmark"]):
                fixture = json.loads((root / "testdata/lm_evaluation_harness" / (spec["benchmark"] + ".json")).read_text())
                self.assertEqual(fixture["parameters"]["num_fewshot"], 0)
                if spec.get("job_ready", True):
                    self.assertEqual(fixture["parameters"]["tokenizer"], "/test_data/tokenizer")
                    self.assertIn("hf", fixture["test_data_ref"])
                else:
                    self.assertNotIn("test_data_ref", fixture)
                self.assertEqual(len(spec["revision"]), 40)
                self.assertTrue(bundle_name(spec).startswith(spec["repo_id"].replace("/", "--")))


if __name__ == "__main__":
    unittest.main()
