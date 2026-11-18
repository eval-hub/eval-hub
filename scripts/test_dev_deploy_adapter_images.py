"""Regression tests for ADAPTER_IMAGES handling in scripts/dev-deploy.sh.

dev-deploy builds every adapter selected by ADAPTERS from a source checkout
unless ADAPTER_IMAGES supplies a prebuilt reference for it. The tests extract
the real parse/selection functions from dev-deploy.sh and drive them with a small
harness, so they pin the contract the deploy relies on:

  1. ADAPTER_IMAGES is a comma-separated list of adapter=image pairs; whitespace
     around entries, names, and references is tolerated, digests and
     registry:port references are accepted, and references may not start with a
     dash or carry shell metacharacters (the value reaches `image pull`);
  2. an override must name an adapter ADAPTERS selects: ignoring it silently
     would deploy the image dev-deploy built while the caller expected the
     prebuilt one, so that case fails instead of building;
  3. an overridden adapter is not built, and a deployment that overrides every
     selected adapter needs no eval-hub-contrib (or lm-evaluation-harness)
     checkout, which is what lets a prebuilt deploy run without those clones;
  4. each adapter keeps one image name (lm-evaluation-harness -> community-lmes,
     the rest -> community-<adapter>), so a prebuilt image is published into the
     same registry path and the recorded manifest stays identical in shape.

Pulling the image, publishing it to the kind-local registry, and recording the
manifest run against a real container runtime and cluster. `adapter_source_image`
and `load_adapter_image` are pinned here against a stub runtime, because they
decide whether a deploy pulls at all and for which platform; publishing and
manifest recording stay covered by the e2e suite.
"""

import os
import pathlib
import re
import subprocess
import tempfile
import unittest

SCRIPTS_DIR = pathlib.Path(__file__).resolve().parent
DEPLOY_PATH = SCRIPTS_DIR / "dev-deploy.sh"

# fail() is extracted too, so the harness reports the script's own diagnostics
# instead of a harness-level failure; log() is stubbed because the tests do not
# assert on timestamps.
DEPLOY_FUNCTIONS = [
    "fail",
    "trim_whitespace",
    "adapter_selected",
    "adapter_image_name",
    "adapter_override_image",
    "adapter_build_required",
    "contrib_adapter_build_required",
    "validate_image_reference",
    "parse_adapters",
    "parse_adapter_images",
]

HARNESS = """#!/usr/bin/env bash
set -Eeuo pipefail
log() {{ printf '[%s] %s\\n' "$1" "$2" >&2; }}
{functions}

ADAPTERS="$1"
ADAPTER_IMAGES="$2"
SELECTED_ADAPTERS=()
SELECTED_ADAPTER_IMAGES=()
ADAPTER_IMAGE_OVERRIDES=()
parse_adapters
parse_adapter_images
printf 'SELECTED=%s\\n' "${{SELECTED_ADAPTERS[*]}}"
printf 'IMAGE_NAMES=%s\\n' "${{SELECTED_ADAPTER_IMAGES[*]}}"
if ((${{#ADAPTER_IMAGE_OVERRIDES[@]}})); then
  printf 'OVERRIDES=%s\\n' "${{ADAPTER_IMAGE_OVERRIDES[*]}}"
else
  printf 'OVERRIDES=\\n'
fi
build=""
for adapter in "${{SELECTED_ADAPTERS[@]}}"; do
  if adapter_build_required "$adapter"; then
    build+="${{adapter}} "
  fi
done
printf 'BUILD=%s\\n' "${{build% }}"
if contrib_adapter_build_required; then
  printf 'CONTRIB_BUILD=true\\n'
else
  printf 'CONTRIB_BUILD=false\\n'
fi
printf 'LMES_IMAGE_NAME=%s\\n' "$(adapter_image_name lm-evaluation-harness)"
"""


def extract_functions(path, names):
    """Return the source of `names` from `path`, comments included."""
    lines = pathlib.Path(path).read_text().splitlines()
    blocks = []
    for name in names:
        start = next((i for i, line in enumerate(lines) if re.match(rf"^{name}\(\) \{{", line)), None)
        assert start is not None, f"{name}() not found in {path}"
        while start > 0 and lines[start - 1].startswith("#"):
            start -= 1
        end = start
        while not lines[end].startswith("}"):
            end += 1
        blocks.extend(lines[start:end + 1])
        blocks.append("")
    return "\n".join(blocks)
PUBLISH_FUNCTIONS = [
    "fail",
    "trim_whitespace",
    "adapter_selected",
    "adapter_image_name",
    "adapter_override_image",
    "validate_image_reference",
    "parse_adapters",
    "parse_adapter_images",
    "image_ref",
    "adapter_source_image",
    "require_local_image",
    "load_adapter_image",
]

# The stub answers `image inspect` from a file of present references, reports a
# configurable architecture for `--format` queries, and records every call, so a
# case can assert what was pulled, for which platform, and whether a present
# image was pulled again.
RUNTIME_STUB = """#!/usr/bin/env bash
printf '%s\\n' "$*" >>"${FAKE_RUNTIME_LOG}"
args=("$@")
ref="${args[${#args[@]}-1]}"
case "${args[0]} ${args[1]}" in
  "image inspect")
    grep -qxF -- "$ref" "${FAKE_RUNTIME_PRESENT}" 2>/dev/null || exit 1
    for arg in "${args[@]}"; do
      if [[ "$arg" == --format ]]; then
        printf '%s\\n' "${FAKE_IMAGE_ARCH:-arm64}"
        exit 0
      fi
    done
    exit 0
    ;;
  "image pull") exit "${FAKE_PULL_EXIT:-0}" ;;
  *) exit 1 ;;
esac
"""

RESOLVE_HARNESS = """#!/usr/bin/env bash
set -Eeuo pipefail
log() {{ printf '[%s] %s\\n' "$1" "$2" >&2; }}
{functions}

IMAGE_TAG=kind-dev
CONTAINER_RUNTIME="$1"
ADAPTERS="$2"
ADAPTER_IMAGES="$3"
SELECTED_ADAPTERS=()
SELECTED_ADAPTER_IMAGES=()
ADAPTER_IMAGE_OVERRIDES=()
parse_adapters
parse_adapter_images
for adapter in "${{SELECTED_ADAPTERS[@]}}"; do
  printf 'SOURCE.%s=%s\\n' "$adapter" "$(adapter_source_image "$adapter")"
done
case "$4" in
  load)
    for adapter in "${{SELECTED_ADAPTERS[@]}}"; do
      load_adapter_image "$adapter" "$(adapter_source_image "$adapter")" \
        "${{ADAPTER_IMAGES_PLATFORM:-}}" "${{FAKE_NODE_ARCH:-}}"
    done
    ;;
  require)
    for adapter in "${{SELECTED_ADAPTERS[@]}}"; do
      require_local_image "$(adapter_source_image "$adapter")"
    done
    ;;
esac
printf 'DONE=yes\\n'
"""






class AdapterImagesTestCase(unittest.TestCase):
    """Runs the real parse/selection functions against ADAPTERS/ADAPTER_IMAGES."""

    @classmethod
    def setUpClass(cls):
        cls.work = tempfile.TemporaryDirectory()
        cls.work_dir = pathlib.Path(cls.work.name)
        cls.harness = cls.work_dir / "case.sh"
        cls.harness.write_text(
            HARNESS.format(functions=extract_functions(DEPLOY_PATH, DEPLOY_FUNCTIONS))
        )

    @classmethod
    def tearDownClass(cls):
        cls.work.cleanup()

    def parse(self, adapters, adapter_images=""):
        """Return (returncode, parsed fields, output) for one selection."""
        proc = subprocess.run(
            ["bash", str(self.harness), adapters, adapter_images],
            capture_output=True,
            text=True,
        )
        fields = {}
        for line in proc.stdout.splitlines():
            key, _, value = line.partition("=")
            fields[key] = value
        return proc.returncode, fields, proc.stdout + proc.stderr

    def assert_parses(self, adapters, adapter_images, **expected):
        code, fields, output = self.parse(adapters, adapter_images)
        self.assertEqual(code, 0, output)
        for key, value in expected.items():
            self.assertEqual(fields[key.upper()], value, f"{key}: {output}")
        return fields

    def assert_rejected(self, adapters, adapter_images, message):
        code, _, output = self.parse(adapters, adapter_images)
        self.assertNotEqual(code, 0, f"expected a failure, got: {output}")
        self.assertIn(message, output)
        return output


class SelectionTests(AdapterImagesTestCase):
    """Without ADAPTER_IMAGES every selected adapter is still built."""

    def test_default_selection_is_built(self):
        self.assert_parses(
            "followbench",
            "",
            selected="followbench",
            image_names="community-followbench",
            overrides="",
            build="followbench",
            contrib_build="true",
        )

    def test_build_order_follows_the_selection(self):
        fields = self.assert_parses(
            "lm-evaluation-harness,followbench",
            "",
            selected="lm-evaluation-harness followbench",
            build="lm-evaluation-harness followbench",
            contrib_build="true",
        )
        self.assertEqual(fields["IMAGE_NAMES"], "community-lmes community-followbench")

    def test_all_selects_every_curated_adapter(self):
        self.assert_parses(
            "all",
            "",
            selected="inspect lm-evaluation-harness lighteval ruler ifbench followbench",
            build="inspect lm-evaluation-harness lighteval ruler ifbench followbench",
        )

    def test_lmes_keeps_its_own_image_name(self):
        fields = self.assert_parses("lm-evaluation-harness", "", build="lm-evaluation-harness")
        self.assertEqual(fields["LMES_IMAGE_NAME"], "community-lmes")


class OverrideTests(AdapterImagesTestCase):
    """An overridden adapter is recorded instead of built."""

    def test_override_replaces_the_build(self):
        self.assert_parses(
            "inspect,lm-evaluation-harness",
            "inspect=quay.io/evalhub/community-inspect@sha256:abc123",
            selected="inspect lm-evaluation-harness",
            overrides="inspect=quay.io/evalhub/community-inspect@sha256:abc123",
            build="lm-evaluation-harness",
            contrib_build="false",
        )

    def test_image_name_is_unchanged_for_an_overridden_adapter(self):
        fields = self.assert_parses(
            "inspect",
            "inspect=quay.io/evalhub/community-inspect:latest",
            image_names="community-inspect",
            build="",
        )
        self.assertEqual(fields["OVERRIDES"], "inspect=quay.io/evalhub/community-inspect:latest")

    def test_overriding_every_selected_adapter_drops_the_contrib_checkout(self):
        self.assert_parses(
            "inspect,ruler",
            "ruler=registry.example.com:5000/evalhub/community-ruler:v1,"
            "inspect=quay.io/x/community-inspect:latest",
            selected="inspect ruler",
            overrides="ruler=registry.example.com:5000/evalhub/community-ruler:v1 "
            "inspect=quay.io/x/community-inspect:latest",
            build="",
            contrib_build="false",
        )

    def test_one_unoverridden_contrib_adapter_keeps_the_checkout(self):
        self.assert_parses(
            "inspect,ifbench",
            "inspect=quay.io/evalhub/community-inspect:latest",
            build="ifbench",
            contrib_build="true",
        )

    def test_overriding_one_of_all_keeps_the_other_builds(self):
        self.assert_parses(
            "all",
            "lighteval=quay.io/evalhub/community-lighteval:latest",
            build="inspect lm-evaluation-harness ruler ifbench followbench",
        )

    def test_lmes_override_needs_no_harness_checkout(self):
        self.assert_parses(
            "lm-evaluation-harness",
            "lm-evaluation-harness=quay.io/evalhub/community-lmes:latest",
            overrides="lm-evaluation-harness=quay.io/evalhub/community-lmes:latest",
            build="",
            contrib_build="false",
        )

    def test_whitespace_around_entries_is_tolerated(self):
        self.assert_parses(
            "inspect,ruler",
            " inspect = quay.io/evalhub/community-inspect:latest ,"
            "  ruler = registry.example.com:5000/evalhub/community-ruler:v1 ",
            selected="inspect ruler",
            overrides="inspect=quay.io/evalhub/community-inspect:latest "
            "ruler=registry.example.com:5000/evalhub/community-ruler:v1",
            build="",
        )


class RejectionTests(AdapterImagesTestCase):
    """A mistaken override fails before any build instead of being ignored."""

    def test_override_for_an_unselected_adapter_is_rejected(self):
        self.assert_rejected(
            "followbench",
            "inspect=quay.io/evalhub/community-inspect:latest",
            "ADAPTERS does not select",
        )

    def test_entry_without_an_image_is_rejected(self):
        self.assert_rejected("inspect", "inspect", "is not an adapter=image pair")

    def test_empty_image_is_rejected(self):
        self.assert_rejected("inspect", "inspect=", "empty image reference")

    def test_empty_entry_is_rejected(self):
        self.assert_rejected(
            "inspect,ruler",
            "inspect=quay.io/evalhub/community-inspect:latest,,"
            "ruler=quay.io/evalhub/community-ruler:latest",
            "ADAPTER_IMAGES contains an empty entry",
        )

    def test_duplicate_override_is_rejected(self):
        self.assert_rejected(
            "inspect",
            "inspect=quay.io/a/community-inspect:latest,inspect=quay.io/b/community-inspect:latest",
            "duplicate entry 'inspect'",
        )

    def test_reference_starting_with_a_dash_is_rejected(self):
        self.assert_rejected("inspect", "inspect=--help", "invalid image reference")

    def test_reference_with_shell_metacharacters_is_rejected(self):
        for image in ("quay.io/x/community-inspect:latest;rm -rf /", "$(id)", "quay.io/x`id`"):
            with self.subTest(image=image):
                self.assert_rejected("inspect", f"inspect={image}", "invalid image reference")


class PrebuiltImageResolutionTests(unittest.TestCase):
    """adapter_source_image and load_adapter_image decide what is pulled."""

    @classmethod
    def setUpClass(cls):
        cls.work = tempfile.TemporaryDirectory()
        cls.work_dir = pathlib.Path(cls.work.name)
        cls.runtime = cls.work_dir / "stub-runtime"
        cls.runtime.write_text(RUNTIME_STUB)
        cls.runtime.chmod(0o755)
        cls.harness = cls.work_dir / "resolve.sh"
        cls.harness.write_text(
            RESOLVE_HARNESS.format(functions=extract_functions(DEPLOY_PATH, PUBLISH_FUNCTIONS))
        )

    @classmethod
    def tearDownClass(cls):
        cls.work.cleanup()

    def run_case(self, adapters, adapter_images, mode, present=(), **extra_env):
        """Run one selection, returning (returncode, sources, runtime calls, output)."""
        present_file = self.work_dir / "present.txt"
        present_file.write_text("".join(f"{ref}\n" for ref in present))
        call_log = self.work_dir / "calls.txt"
        call_log.write_text("")
        env = {
            "PATH": str(self.work_dir) + ":" + os.environ["PATH"],
            "FAKE_RUNTIME_PRESENT": str(present_file),
            "FAKE_RUNTIME_LOG": str(call_log),
        }
        for key, value in extra_env.items():
            env[key.upper()] = str(value)
        proc = subprocess.run(
            ["bash", str(self.harness), str(self.runtime), adapters, adapter_images, mode],
            capture_output=True,
            text=True,
            env=env,
        )
        sources = {}
        for line in proc.stdout.splitlines():
            if line.startswith("SOURCE."):
                key, _, value = line.partition("=")
                sources[key[len("SOURCE."):]] = value
        calls = [line for line in call_log.read_text().splitlines() if line]
        return proc.returncode, sources, calls, proc.stdout + proc.stderr

    def test_an_override_is_the_published_source(self):
        override = "quay.io/evalhub/community-inspect@sha256:abc123"
        code, sources, _, output = self.run_case(
            "inspect", f"inspect={override}", "load", present=(override,)
        )
        self.assertEqual(code, 0, output)
        self.assertEqual(sources["inspect"], override)

    def test_a_built_adapter_uses_the_local_dev_tag(self):
        code, sources, _, output = self.run_case(
            "inspect", "", "load", present=("localhost/evalhub/community-inspect:kind-dev",)
        )
        self.assertEqual(code, 0, output)
        self.assertEqual(sources["inspect"], "localhost/evalhub/community-inspect:kind-dev")

    def test_a_present_image_is_not_pulled(self):
        override = "quay.io/evalhub/community-inspect:latest"
        code, _, calls, output = self.run_case(
            "inspect", f"inspect={override}", "load", present=(override,)
        )
        self.assertEqual(code, 0, output)
        self.assertFalse(
            [call for call in calls if call.startswith("image pull")],
            f"a present image was pulled: {calls}",
        )

    def test_a_missing_image_is_pulled_once(self):
        override = "quay.io/evalhub/community-inspect:latest"
        code, _, calls, output = self.run_case("inspect", f"inspect={override}", "load")
        self.assertEqual(code, 0, output)
        self.assertEqual(
            [call for call in calls if call.startswith("image pull")],
            [f"image pull {override}"],
            calls,
        )

    def test_skip_build_verification_fails_for_a_missing_image(self):
        override = "quay.io/evalhub/community-inspect:latest"
        code, _, calls, output = self.run_case("inspect", f"inspect={override}", "require")
        self.assertNotEqual(code, 0, f"expected a missing-image failure, got: {output}")
        self.assertIn(f"Required local image is missing: {override}", output)
        self.assertFalse(
            [call for call in calls if call.startswith("image pull")],
            f"SKIP_BUILD must not pull: {calls}",
        )

    def test_a_pull_uses_the_host_platform_by_default(self):
        override = "quay.io/evalhub/community-inspect:latest"
        code, _, calls, output = self.run_case("inspect", f"inspect={override}", "load")
        self.assertEqual(code, 0, output)
        self.assertEqual(
            [call for call in calls if call.startswith("image pull")],
            [f"image pull {override}"],
            calls,
        )

    def test_a_platform_pulls_that_platform(self):
        """The curated community images are amd64 only, so an arm64 host opts in."""
        override = "quay.io/evalhub/community-inspect:latest"
        code, _, calls, output = self.run_case(
            "inspect", f"inspect={override}", "load", ADAPTER_IMAGES_PLATFORM="linux/amd64"
        )
        self.assertEqual(code, 0, output)
        self.assertEqual(
            [call for call in calls if call.startswith("image pull")],
            [f"image pull --platform linux/amd64 {override}"],
            calls,
        )

    def test_a_foreign_architecture_warns_about_emulation(self):
        override = "quay.io/evalhub/community-inspect:latest"
        code, _, _, output = self.run_case(
            "inspect",
            f"inspect={override}",
            "load",
            present=(override,),
            FAKE_IMAGE_ARCH="amd64",
            FAKE_NODE_ARCH="arm64",
        )
        self.assertEqual(code, 0, output)
        self.assertIn("run under emulation", output)

    def test_a_matching_architecture_is_silent(self):
        override = "quay.io/evalhub/community-inspect:latest"
        code, _, _, output = self.run_case(
            "inspect",
            f"inspect={override}",
            "load",
            present=(override,),
            FAKE_IMAGE_ARCH="arm64",
            FAKE_NODE_ARCH="arm64",
        )
        self.assertEqual(code, 0, output)
        self.assertNotIn("emulation", output)

    def test_a_failed_pull_suggests_the_platform_input(self):
        override = "quay.io/evalhub/community-inspect:latest"
        code, _, _, output = self.run_case(
            "inspect", f"inspect={override}", "load", FAKE_PULL_EXIT=1
        )
        self.assertNotEqual(code, 0, f"expected a pull failure, got: {output}")
        self.assertIn("ADAPTER_IMAGES_PLATFORM=linux/amd64", output)
        self.assertIn("dev-deploy builds it from source", output)


if __name__ == "__main__":
    unittest.main()
