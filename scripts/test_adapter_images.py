"""Regression tests for scripts/adapter-images.sh.

The script turns ADAPTERS into the ADAPTER_IMAGES value a deploy accepts by
reading each adapter's image from its provider YAML, so these tests run it with
no container runtime and no network and pin:

  1. the pair list and its order: the curated order for ADAPTERS=all, the
     selection order otherwise, and followbench when ADAPTERS is unset;
  2. the output shape: one comma-separated line on stdout, unquoted, with the
     diagnostics on stderr and nothing printed when the run fails;
  3. that the reference is copied exactly as the provider declares it - a
     `latest` tag stays `latest`, a version tag stays that tag, and a digest is
     reproduced as pinned - because the target must not silently rewrite what a
     deploy runs; a YAML comment after the scalar is dropped instead of becoming
     part of the reference, and the pair it produces is accepted by dev-deploy's
     image-reference validation;
  4. the rejections that keep a wrong pair out of a deploy: an unsupported or
     duplicate adapter, a missing provider file, and a provider without a
     runtime image;
  5. that it runs under the stock macOS `/bin/bash`, which is still 3.2, because
     `make test-benchmarks-kind-latest` substitutes this script's output on a
     fresh Mac and bash 3.2 aborts on an empty `"${array[@]}"` under `set -u`.

The provider YAMLs in config/providers are read directly, so the expected
references come from the shipped configuration instead of a copy here.
"""

import os
import pathlib
import subprocess
import tempfile
import unittest

from test_dev_deploy_adapter_images import DEPLOY_PATH, extract_functions

SCRIPTS_DIR = pathlib.Path(__file__).resolve().parent
SCRIPT_PATH = SCRIPTS_DIR / "adapter-images.sh"

# The emitted pair is only useful if the deploy accepts it, so the acceptance
# check runs dev-deploy's own validator instead of restating its grammar here.
VALIDATE_HARNESS = """#!/usr/bin/env bash
set -Eeuo pipefail
{functions}
validate_image_reference "$1"
"""


def image_reference_is_accepted(reference):
    """Return True when dev-deploy's validate_image_reference accepts `reference`."""
    with tempfile.TemporaryDirectory() as tmp:
        harness = pathlib.Path(tmp) / "validate.sh"
        harness.write_text(
            VALIDATE_HARNESS.format(
                functions=extract_functions(DEPLOY_PATH, ["validate_image_reference"])
            )
        )
        proc = subprocess.run(
            ["bash", str(harness), reference], capture_output=True, text=True
        )
        return proc.returncode == 0

PROVIDER_FILES = {
    "inspect": "inspect.yaml",
    "lm-evaluation-harness": "lm_evaluation_harness.yaml",
    "lighteval": "lighteval.yaml",
    "ruler": "ruler.yaml",
    "ifbench": "ifbench.yaml",
    "followbench": "followbench.yaml",
}

CURATED_ORDER = [
    "inspect",
    "lm-evaluation-harness",
    "lighteval",
    "ruler",
    "ifbench",
    "followbench",
]

PROVIDER_TEMPLATE = """id: {adapter}
runtime:
  k8s:
    image: {image}
"""


def provider_image(adapter, providers_dir=None):
    """Return the image the provider YAML declares for an adapter."""
    directory = providers_dir or (SCRIPTS_DIR.parent / "config" / "providers")
    lines = (directory / PROVIDER_FILES[adapter]).read_text().splitlines()
    for line in lines:
        stripped = line.strip()
        if stripped.startswith("image:"):
            return stripped[len("image:"):].strip()
    raise AssertionError(f"no image in provider config for {adapter}")


class AdapterImagesScriptTests(unittest.TestCase):
    """Runs scripts/adapter-images.sh against the shipped provider configs."""

    @classmethod
    def setUpClass(cls):
        cls.work = tempfile.TemporaryDirectory()
        cls.work_dir = pathlib.Path(cls.work.name)

    @classmethod
    def tearDownClass(cls):
        cls.work.cleanup()

    def provider_dir(self, images):
        """Create a providers directory holding `{adapter: image}` YAML files."""
        directory = pathlib.Path(tempfile.mkdtemp(dir=self.work_dir))
        for adapter, image in images.items():
            (directory / PROVIDER_FILES[adapter]).write_text(
                PROVIDER_TEMPLATE.format(adapter=adapter, image=image)
            )
        return directory

    def run_script(self, adapters, providers_dir=None, shell="bash"):
        """Run the script once; returns (returncode, stdout, stderr)."""
        env = {
            "PATH": os.environ["PATH"],
            "ADAPTER_IMAGE_PROVIDERS_DIR": str(providers_dir or SCRIPTS_DIR.parent / "config" / "providers"),
        }
        if adapters is not None:
            env["ADAPTERS"] = adapters
        proc = subprocess.run(
            [shell, str(SCRIPT_PATH)],
            capture_output=True,
            text=True,
            env=env,
        )
        return proc.returncode, proc.stdout, proc.stderr

    def assert_pairs(self, adapters, expected_adapters, providers_dir=None):
        code, out, err = self.run_script(adapters, providers_dir)
        self.assertEqual(code, 0, err)
        expected = ",".join(
            f"{adapter}={provider_image(adapter, providers_dir)}" for adapter in expected_adapters
        ) + "\n"
        self.assertEqual(out, expected, err)
        self.assertEqual(len(out.splitlines()), 1, out)
        return out

    def assert_rejected(self, adapters, message, providers_dir=None):
        code, out, err = self.run_script(adapters, providers_dir)
        self.assertNotEqual(code, 0, f"expected a failure, got stdout={out!r} stderr={err!r}")
        self.assertIn(message, err)
        self.assertEqual(out, "", f"a failed run must not print a partial value: {out!r}")

    def test_all_adapters_are_emitted_in_curated_order(self):
        self.assert_pairs("all", CURATED_ORDER)

    def test_a_subset_keeps_the_selection_order(self):
        self.assert_pairs("ruler,inspect", ["ruler", "inspect"])

    def test_the_default_selection_is_followbench(self):
        self.assert_pairs(None, ["followbench"])

    def test_whitespace_around_entries_is_tolerated(self):
        self.assert_pairs(" inspect , ruler ", ["inspect", "ruler"])

    def test_latest_stays_a_floating_tag(self):
        out = self.assert_pairs("followbench", ["followbench"])
        self.assertTrue(out.endswith("community-followbench:latest\n"), out)

    def test_a_version_tag_is_reproduced_as_declared(self):
        out = self.assert_pairs("lm-evaluation-harness", ["lm-evaluation-harness"])
        self.assertIn("quay.io/opendatahub/ta-lmes-job:", out)
        self.assertNotIn("@", out)

    def test_a_digest_pin_is_reproduced_as_pinned(self):
        pinned = "quay.io/evalhub/community-inspect@sha256:" + "1" * 64
        out = self.assert_pairs(
            "inspect", ["inspect"], providers_dir=self.provider_dir({"inspect": pinned})
        )
        self.assertEqual(out, f"inspect={pinned}\n")

    def test_a_quoted_image_with_a_trailing_comment_is_accepted_as_a_pair(self):
        """YAML allows a comment after a quoted scalar; it is not part of the value."""
        providers = self.provider_dir(
            {"inspect": '"quay.io/evalhub/community-inspect:latest" # default'}
        )
        code, out, err = self.run_script("inspect", providers)
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "inspect=quay.io/evalhub/community-inspect:latest\n")
        reference = out.strip().split("=", 1)[1]
        self.assertTrue(
            image_reference_is_accepted(reference),
            f"dev-deploy rejects the generated reference: {reference}",
        )

    def test_a_plain_image_with_a_trailing_comment_drops_the_comment(self):
        providers = self.provider_dir(
            {"ruler": "quay.io/evalhub/community-ruler:latest # curated"}
        )
        code, out, err = self.run_script("ruler", providers)
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "ruler=quay.io/evalhub/community-ruler:latest\n")

    def test_a_single_quoted_image_with_a_trailing_comment_drops_the_comment(self):
        providers = self.provider_dir(
            {"ifbench": "'quay.io/evalhub/community-ifbench:latest' # curated"}
        )
        code, out, err = self.run_script("ifbench", providers)
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "ifbench=quay.io/evalhub/community-ifbench:latest\n")

    def test_a_quoted_digest_with_a_comment_stays_pinned(self):
        pinned = "quay.io/evalhub/community-inspect@sha256:" + "1" * 64
        providers = self.provider_dir({"inspect": f'"{pinned}"  # pinned for this release'})
        code, out, err = self.run_script("inspect", providers)
        self.assertEqual(code, 0, err)
        self.assertEqual(out, f"inspect={pinned}\n")
        self.assertTrue(image_reference_is_accepted(pinned))

    def test_the_value_is_unquoted_so_it_can_be_substituted(self):
        out = self.assert_pairs("inspect,ruler", ["inspect", "ruler"])
        self.assertNotIn("'", out)
        self.assertNotIn('"', out)
        self.assertNotIn(" ", out)

    def test_a_duplicate_adapter_is_rejected(self):
        self.assert_rejected("inspect,inspect", "duplicate entry 'inspect'")

    def test_an_unsupported_adapter_is_rejected(self):
        self.assert_rejected("garak", "Unsupported adapter 'garak'")

    def test_an_empty_entry_is_rejected(self):
        self.assert_rejected("inspect,,ruler", "ADAPTERS contains an empty entry")

    def test_a_missing_provider_file_is_rejected(self):
        providers = self.provider_dir({"inspect": "quay.io/evalhub/community-inspect:latest"})
        self.assert_rejected(
            "inspect,ruler",
            "Provider configuration not found for adapter 'ruler'",
            providers_dir=providers,
        )

    def test_a_provider_without_a_runtime_image_is_rejected(self):
        providers = pathlib.Path(tempfile.mkdtemp(dir=self.work_dir))
        (providers / PROVIDER_FILES["inspect"]).write_text("id: inspect\nruntime:\n  k8s:\n")
        self.assert_rejected(
            "inspect",
            "has no runtime image for adapter 'inspect'",
            providers_dir=providers,
        )


class PortabilityTests(AdapterImagesScriptTests):
    """The script runs under the stock macOS /bin/bash, which is still 3.2.

    `make test-benchmarks-kind-latest` substitutes this script's output on a
    fresh Mac, so it has to work under /bin/bash: bash 3.2 treats an empty
    "${array[@]}" as an unbound variable under `set -u`, which is why the
    duplicate check is indexed.
    """

    def system_bash(self):
        shell = pathlib.Path("/bin/bash")
        if not shell.exists():
            self.skipTest("/bin/bash is not available")
        return str(shell)

    def test_all_adapters_are_emitted_under_system_bash(self):
        code, out, err = self.run_script("all", shell=self.system_bash())
        self.assertEqual(code, 0, err)
        expected = ",".join(
            f"{adapter}={provider_image(adapter)}" for adapter in CURATED_ORDER
        ) + "\n"
        self.assertEqual(out, expected, err)

    def test_a_duplicate_is_rejected_under_system_bash(self):
        code, out, err = self.run_script("inspect,inspect", shell=self.system_bash())
        self.assertNotEqual(code, 0, f"expected a duplicate failure, got stdout={out!r}")
        self.assertIn("duplicate entry 'inspect'", err)
        self.assertNotIn("unbound variable", err)
        self.assertEqual(out, "")


if __name__ == "__main__":
    unittest.main()
