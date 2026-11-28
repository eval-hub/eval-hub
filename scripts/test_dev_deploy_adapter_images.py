"""Regression tests for ADAPTER_IMAGES handling in scripts/dev-deploy.sh.

dev-deploy builds every adapter selected by ADAPTERS from a source checkout
unless ADAPTER_IMAGES supplies a prebuilt reference for it. The tests extract
the real parse/selection functions from dev-deploy.sh and drive them with a small
harness, so they pin the contract the deploy relies on:

  1. ADAPTER_IMAGES is a comma-separated list of adapter=image pairs; whitespace
     around entries, names, and references is tolerated, digests and
     registry:port references are accepted, and references may not start with a
     dash or carry shell metacharacters (the value reaches `image pull`). A
     structurally invalid reference (a trailing colon with no tag, an empty path
     element, an empty digest) is rejected during input validation, before either
     core build starts, and so is a value with an embedded newline: `read`
     consumes one line, so the pairs after it would be dropped and their adapters
     built from source instead of using the supplied image;
  2. an override must name an adapter ADAPTERS selects: ignoring it silently
     would deploy the image dev-deploy built while the caller expected the
     prebuilt one, so that case fails instead of building;
  3. an overridden adapter is not built, and a deployment that overrides every
     selected adapter needs no eval-hub-contrib (or lm-evaluation-harness)
     checkout, which is what lets a prebuilt deploy run without those clones;
  4. each adapter keeps one image name (lm-evaluation-harness -> community-lmes,
     the rest -> community-<adapter>), so a prebuilt image is published into the
     same registry path and the recorded manifest stays identical in shape;
  5. an override that names a local tag this run builds from source is rejected
     with the rest of the input validation, before the first build, because that
     build would replace the prebuilt image and the adapter would be deployed
     with the image built for another adapter. An override that names its own
     adapter's tag stays valid, and SKIP_BUILD=true builds nothing, so its reuse
     still works.

Pulling the image, publishing it to the kind-local registry, and recording the
manifest run against a real container runtime and cluster. `adapter_source_image`
and `load_adapter_image` are pinned here against a stub runtime, because they
decide whether a deploy pulls at all and for which platform - including whether a
cached image of another architecture is replaced when ADAPTER_IMAGES_PLATFORM
asks for a specific one; publishing and manifest recording stay covered by the
e2e suite.
"""

import os
import pathlib
import re
import subprocess
import tempfile
import unittest

SCRIPTS_DIR = pathlib.Path(__file__).resolve().parent
DEPLOY_PATH = SCRIPTS_DIR / "dev-deploy.sh"
# The curated adapter list, the image names, and the per-adapter build recipe
# live in the library shared with build-adapter-images, so a harness that drives
# dev-deploy's selection has to read both files.
LIB_PATH = SCRIPTS_DIR / "lib" / "adapter-build.sh"
# Functions the library defines, so a harness reads each one from its own file.
LIB_FUNCTIONS = [
    "curated_adapters",
    "curated_adapter_list",
    "curated_adapter_supported",
    "adapter_image_name",
    "adapter_build_context",
    "adapter_containerfile",
    "build_adapter_image",
]


# fail() is extracted too, so the harness reports the script's own diagnostics
# instead of a harness-level failure; log() is stubbed because the tests do not
# assert on timestamps.
DEPLOY_FUNCTIONS = [
    "fail",
    "trim_whitespace",
    "curated_adapters",
    "curated_adapter_list",
    "curated_adapter_supported",
    "adapter_selected",
    "adapter_image_name",
    "adapter_override_image",
    "adapter_build_required",
    "contrib_adapter_build_required",
    "validate_image_reference",
    "parse_adapters",
    "parse_adapter_images",
    "image_ref",
    "validate_adapter_image_tag_collisions",
]

HARNESS = """#!/usr/bin/env bash
set -Eeuo pipefail
log() {{ printf '[%s] %s\\n' "$1" "$2" >&2; }}
{functions}

ADAPTERS="$1"
ADAPTER_IMAGES="$2"
IMAGE_TAG="${{IMAGE_TAG:-kind-dev}}"
SKIP_BUILD="${{SKIP_BUILD:-false}}"
SELECTED_ADAPTERS=()
SELECTED_ADAPTER_IMAGES=()
ADAPTER_IMAGE_OVERRIDES=()
parse_adapters
parse_adapter_images
validate_adapter_image_tag_collisions
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


def deploy_functions(names):
    """Return the source of `names`, reading the shared library for what moved there.

    A name that lives in scripts/lib/adapter-build.sh is read from that file, and
    every other name from dev-deploy.sh, so the harnesses follow the functions to
    their new home instead of restating them.
    """
    lib_names = [name for name in names if name in LIB_FUNCTIONS]
    deploy_names = [name for name in names if name not in LIB_FUNCTIONS]
    blocks = []
    if lib_names:
        blocks.append(extract_functions(LIB_PATH, lib_names))
    if deploy_names:
        blocks.append(extract_functions(DEPLOY_PATH, deploy_names))
    return "\n".join(blocks)


PUBLISH_FUNCTIONS = [
    "fail",
    "trim_whitespace",
    "curated_adapters",
    "curated_adapter_list",
    "curated_adapter_supported",
    "adapter_selected",
    "adapter_image_name",
    "adapter_override_image",
    "validate_image_reference",
    "parse_adapters",
    "parse_adapter_images",
    "image_ref",
    "adapter_source_image",
    "require_local_image",
    "platform_arch",
    "cached_image_arch",
    "load_adapter_image",
]

# The stub answers `image inspect` from a file of present references, reports a
# configurable architecture for `--format` queries, and records every call, so a
# case can assert what was pulled, for which platform, and whether a present
# image was pulled again. A successful `image pull --platform` records the
# variant it pulled, so a later inspect reports the architecture the pull left on
# the reference, the way a real engine replaces the tag.
RUNTIME_STUB = """#!/usr/bin/env bash
printf '%s\\n' "$*" >>"${FAKE_RUNTIME_LOG}"
args=("$@")
ref="${args[${#args[@]}-1]}"
state="${FAKE_RUNTIME_STATE:-}"
case "${args[0]} ${args[1]}" in
  "image inspect")
    grep -qxF -- "$ref" "${FAKE_RUNTIME_PRESENT}" 2>/dev/null || exit 1
    for arg in "${args[@]}"; do
      if [[ "$arg" == --format ]]; then
        arch="${FAKE_IMAGE_ARCH:-arm64}"
        if [[ -n "$state" ]]; then
          recorded="$(awk -v ref="$ref" '$2 == ref { print $1; exit }' "$state" 2>/dev/null || true)"
          [[ -z "$recorded" ]] || arch="$recorded"
        fi
        printf '%s\\n' "$arch"
        exit 0
      fi
    done
    exit 0
    ;;
  "image pull")
    status="${FAKE_PULL_EXIT:-0}"
    if [[ "$status" == 0 && -n "$state" ]]; then
      previous=""
      for arg in "${args[@]}"; do
        if [[ "$previous" == --platform ]]; then
          printf '%s %s\\n' "${arg#linux/}" "$ref" >>"$state"
        fi
        previous="$arg"
      done
    fi
    exit "$status"
    ;;
  "image tag") exit 0 ;;
  "image push") exit 0 ;;
  "image rm") exit 0 ;;
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

# tag_and_push publishes one local reference into the kind-local registry. The
# caller's image must survive a publish whose source already equals the
# destination, or a later SKIP_BUILD=true deployment fails its local-image check,
# and a destination another adapter still needs as its source must be refused
# instead of re-tagged or removed.
TAG_AND_PUSH_FUNCTIONS = [
    "fail",
    "image_ref_protected",
    "remove_temporary_registry_tag",
    "tag_and_push",
]

PUBLISH_HARNESS = """#!/usr/bin/env bash
set -Eeuo pipefail
log() {{ printf '[%s] %s\\n' "$1" "$2" >&2; }}
{functions}

TEMP_REGISTRY_TAGS=()
PROTECTED_IMAGE_REFS=()
CONTAINER_RUNTIME="$1"
if [[ -n "${{4:-}}" ]]; then
  IFS=, read -r -a PROTECTED_IMAGE_REFS <<<"$4"
fi
tag_and_push "$2" "$3"
printf 'PENDING_TAGS=%s\\n' "${{TEMP_REGISTRY_TAGS[*]}}"
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
            HARNESS.format(functions=deploy_functions(DEPLOY_FUNCTIONS))
        )

    @classmethod
    def tearDownClass(cls):
        cls.work.cleanup()

    def parse(self, adapters, adapter_images="", environment=None, shell="bash"):
        """Return (returncode, parsed fields, output) for one selection."""
        env = dict(os.environ)
        # Pin the inputs the harness reads beyond its arguments, so a developer's
        # exported SKIP_BUILD or IMAGE_TAG cannot change what a case asserts.
        env["SKIP_BUILD"] = "false"
        env["IMAGE_TAG"] = "kind-dev"
        env.update(environment or {})
        proc = subprocess.run(
            [shell, str(self.harness), adapters, adapter_images],
            capture_output=True,
            text=True,
            env=env,
        )
        fields = {}
        for line in proc.stdout.splitlines():
            key, _, value = line.partition("=")
            fields[key] = value
        return proc.returncode, fields, proc.stdout + proc.stderr

    def assert_parses(self, adapters, adapter_images, environment=None, **expected):
        code, fields, output = self.parse(adapters, adapter_images, environment)
        self.assertEqual(code, 0, output)
        for key, value in expected.items():
            self.assertEqual(fields[key.upper()], value, f"{key}: {output}")
        return fields

    def assert_rejected(self, adapters, adapter_images, message, environment=None):
        code, _, output = self.parse(adapters, adapter_images, environment)
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

    def test_a_trailing_colon_without_a_tag_is_rejected(self):
        self.assert_rejected("inspect", "inspect=quay.io/x/inspect:", "invalid image reference")

    def test_an_empty_path_element_is_rejected(self):
        self.assert_rejected(
            "inspect", "inspect=quay.io//community-inspect:latest", "invalid image reference"
        )

    def test_a_trailing_slash_is_rejected(self):
        self.assert_rejected("inspect", "inspect=quay.io/x/", "invalid image reference")

    def test_an_empty_digest_is_rejected(self):
        self.assert_rejected(
            "inspect", "inspect=quay.io/x/community-inspect@", "invalid image reference"
        )

    def test_an_embedded_newline_is_rejected_before_any_build(self):
        """`read` takes one line, so later pairs would silently build from source."""
        self.assert_rejected(
            "inspect,ruler",
            "inspect=quay.io/x/inspect:v1,\nruler=quay.io/x/ruler:v1",
            "ADAPTER_IMAGES must be a single line",
        )


class TagCollisionTests(AdapterImagesTestCase):
    """An override must not name a local tag this run's own build replaces.

    build_images runs before publish_images, so a source build that writes the
    same tag would leave the override publishing the image built for another
    adapter. The conflict fails with input validation, before the first build.
    """

    def test_override_of_a_built_adapter_tag_is_rejected(self):
        output = self.assert_rejected(
            "inspect,followbench",
            "inspect=localhost/evalhub/community-followbench:kind-dev",
            "the local tag this deployment builds",
        )
        self.assertIn("community-followbench", output)
        self.assertIn("drop 'followbench' from ADAPTERS", output)

    def test_override_of_a_core_image_tag_is_rejected(self):
        for image in (
            "localhost/evalhub/eval-hub:kind-dev",
            "localhost/evalhub/trustyai-service-operator:kind-dev",
        ):
            with self.subTest(image=image):
                self.assert_rejected(
                    "inspect",
                    f"inspect={image}",
                    "the local tag this deployment builds",
                )

    def test_override_of_its_own_source_tag_stays_valid(self):
        """That adapter is not built, so the tag it names is never replaced."""
        reference = "localhost/evalhub/community-inspect:kind-dev"
        self.assert_parses(
            "inspect",
            f"inspect={reference}",
            overrides=f"inspect={reference}",
            build="",
        )

    def test_skip_build_reuse_allows_a_core_image_tag(self):
        """SKIP_BUILD=true builds nothing, so no local tag is replaced."""
        self.assert_parses(
            "inspect",
            "inspect=localhost/evalhub/eval-hub:kind-dev",
            environment={"SKIP_BUILD": "true"},
            build="",
        )


class ReferenceFormTests(AdapterImagesTestCase):
    """References the container runtime accepts keep working."""

    def test_valid_reference_forms_are_accepted(self):
        digest = "sha256:" + "1" * 64
        for image in (
            "python",
            "python:3.12-slim",
            "quay.io/evalhub/community-inspect:latest",
            "registry.example.com:5000/evalhub/community-ruler:v1",
            "localhost:18443/evalhub/community-inspect:kind-dev",
            "quay.io/evalhub/community-inspect@" + digest,
            "quay.io/evalhub/community-inspect:v1@" + digest,
        ):
            with self.subTest(image=image):
                self.assert_parses("inspect", f"inspect={image}", overrides=f"inspect={image}")


class PortabilityTests(AdapterImagesTestCase):
    """The selection runs under the stock macOS /bin/bash, which is still 3.2.

    `make test-benchmarks-kind-latest` deploys through dev-deploy on a fresh Mac,
    so the selection has to work under /bin/bash: bash 3.2 treats an empty
    "${array[@]}" as an unbound variable under `set -u`, which is why
    adapter_selected is indexed.
    """

    def system_bash(self):
        shell = pathlib.Path("/bin/bash")
        if not shell.exists():
            self.skipTest("/bin/bash is not available")
        return str(shell)

    def test_a_selection_runs_under_system_bash(self):
        code, fields, output = self.parse("all", shell=self.system_bash())
        self.assertEqual(code, 0, output)
        self.assertEqual(
            fields["SELECTED"],
            "inspect lm-evaluation-harness lighteval ruler ifbench followbench",
        )

    def test_a_duplicate_is_rejected_under_system_bash(self):
        code, _, output = self.parse("inspect,inspect", shell=self.system_bash())
        self.assertNotEqual(code, 0, f"expected a duplicate failure, got: {output}")
        self.assertIn("duplicate entry 'inspect'", output)
        self.assertNotIn("unbound variable", output)


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
            RESOLVE_HARNESS.format(functions=deploy_functions(PUBLISH_FUNCTIONS))
        )
        cls.publish_harness = cls.work_dir / "publish.sh"
        cls.publish_harness.write_text(
            PUBLISH_HARNESS.format(functions=extract_functions(DEPLOY_PATH, TAG_AND_PUSH_FUNCTIONS))
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
        # The pull state lets a case assert what a pull leaves behind for a
        # later inspect, for example the architecture of a replaced variant.
        state_file = self.work_dir / "state.txt"
        state_file.write_text("")
        env = {
            "PATH": str(self.work_dir) + ":" + os.environ["PATH"],
            "FAKE_RUNTIME_PRESENT": str(present_file),
            "FAKE_RUNTIME_LOG": str(call_log),
            "FAKE_RUNTIME_STATE": str(state_file),
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

    def run_publish(self, source, destination, present=(), protected=()):
        """Publish one reference pair; returns (returncode, runtime calls, output)."""
        present_file = self.work_dir / "publish-present.txt"
        present_file.write_text("".join(f"{ref}\n" for ref in present))
        call_log = self.work_dir / "publish-calls.txt"
        call_log.write_text("")
        env = {
            "PATH": str(self.work_dir) + ":" + os.environ["PATH"],
            "FAKE_RUNTIME_PRESENT": str(present_file),
            "FAKE_RUNTIME_LOG": str(call_log),
        }
        proc = subprocess.run(
            [
                "bash",
                str(self.publish_harness),
                str(self.runtime),
                source,
                destination,
                ",".join(protected),
            ],
            capture_output=True,
            text=True,
            env=env,
        )
        calls = [line for line in call_log.read_text().splitlines() if line]
        return proc.returncode, calls, proc.stdout + proc.stderr

    def test_a_same_reference_publish_keeps_the_caller_tag(self):
        """An override already equal to the registry path must not be removed."""
        reference = "localhost:5000/evalhub/community-inspect:kind-dev"
        code, calls, output = self.run_publish(
            reference, reference, present=(reference,), protected=(reference,)
        )
        self.assertEqual(code, 0, output)
        self.assertIn(f"image push {reference}", calls, calls)
        self.assertFalse(
            [call for call in calls if call.startswith(("image tag", "image rm"))],
            f"an identical reference was re-tagged or removed: {calls}",
        )

    def test_a_distinct_destination_is_still_tagged_and_cleaned_up(self):
        source = "quay.io/evalhub/community-inspect:latest"
        destination = "localhost:5000/evalhub/community-inspect:kind-dev"
        code, calls, output = self.run_publish(source, destination, present=(source,))
        self.assertEqual(code, 0, output)
        self.assertIn(f"image tag {source} {destination}", calls, calls)
        self.assertIn(f"image rm {destination}", calls, calls)

    def test_a_destination_still_needed_as_a_source_is_refused(self):
        """Another adapter's override can name a path this deploy publishes to."""
        source = "quay.io/evalhub/community-inspect:latest"
        destination = "localhost:5000/evalhub/community-ruler:kind-dev"
        code, calls, output = self.run_publish(
            source, destination, present=(source,), protected=(destination,)
        )
        self.assertNotEqual(code, 0, f"expected a protected-destination failure, got: {output}")
        self.assertIn("still needed as the source image of another adapter", output)
        self.assertFalse(
            [call for call in calls if call.startswith(("image tag", "image rm", "image push"))],
            f"a protected reference was re-tagged, removed, or pushed: {calls}",
        )

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

    def test_a_cached_image_of_another_architecture_is_pulled_for_the_platform(self):
        """A cache hit must not override an explicit ADAPTER_IMAGES_PLATFORM."""
        override = "quay.io/evalhub/community-inspect:latest"
        code, _, calls, output = self.run_case(
            "inspect",
            f"inspect={override}",
            "load",
            present=(override,),
            FAKE_IMAGE_ARCH="arm64",
            ADAPTER_IMAGES_PLATFORM="linux/amd64",
        )
        self.assertEqual(code, 0, output)
        self.assertEqual(
            [call for call in calls if call.startswith("image pull")],
            [f"image pull --platform linux/amd64 {override}"],
            calls,
        )
        self.assertIn("linux/arm64, not the requested linux/amd64", output)

    def test_a_cached_image_of_the_requested_platform_is_kept(self):
        override = "quay.io/evalhub/community-inspect:latest"
        code, _, calls, output = self.run_case(
            "inspect",
            f"inspect={override}",
            "load",
            present=(override,),
            FAKE_IMAGE_ARCH="amd64",
            ADAPTER_IMAGES_PLATFORM="linux/amd64",
        )
        self.assertEqual(code, 0, output)
        self.assertFalse(
            [call for call in calls if call.startswith("image pull")],
            f"a matching cached image was pulled again: {calls}",
        )

    def test_replacing_a_cached_variant_still_warns_about_emulation(self):
        """The pulled variant is foreign to the nodes, so the warning must fire."""
        override = "quay.io/evalhub/community-inspect:latest"
        code, _, calls, output = self.run_case(
            "inspect",
            f"inspect={override}",
            "load",
            present=(override,),
            FAKE_IMAGE_ARCH="arm64",
            ADAPTER_IMAGES_PLATFORM="linux/amd64",
            FAKE_NODE_ARCH="arm64",
        )
        self.assertEqual(code, 0, output)
        self.assertEqual(
            [call for call in calls if call.startswith("image pull")],
            [f"image pull --platform linux/amd64 {override}"],
            calls,
        )
        self.assertIn("run under emulation", output)

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
