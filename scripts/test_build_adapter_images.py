"""Regression tests for scripts/build-adapter-images.sh.

The script builds the curated adapter images for this machine's architecture and
pushes them to a registry the caller owns, printing the ADAPTER_IMAGES value for
the pushed images. A container runtime and a registry are stubbed, so the tests
pin the behaviour that keeps a push usable without building anything:

  1. the build target comes from the container runtime, not the host shell: a
     `podman machine --arch amd64` on an arm64 Mac must build amd64, because the
     opposite mismatch is what puts the adapter Pods back under qemu;
  2. every image is built from the checkout, Containerfile, and image name
     dev-deploy uses, with `--platform` always explicit, and only the LM
     Evaluation Harness image receives TARGETOS/TARGETARCH - a pushed image must
     be the image a deploy would have built for itself;
  3. the destination is <registry>/<namespace>/<adapter image name>:<tag>, and an
     unknown namespace falls back to the account the runtime is logged in to, so
     a personal Quay registry needs no hard-coded user name;
  4. a registry it cannot accept fails before the first build: a tag or digest,
     an upper-case or nested namespace, an unsupported or duplicate adapter, and
     a missing source checkout all produce a diagnostic and no push, because a
     half-built push leaves a registry holding images a deploy cannot use;
  5. stdout carries exactly one line - the ADAPTER_IMAGES value - with every
     diagnostic on stderr, so `ADAPTER_IMAGES="$(make -s adapter-images-push)"`
     cannot pick up log output. The runtime's own build and push progress is
     written to stdout by the engine, so it is redirected to stderr too;
  6. a destination always names both a registry and a namespace, and the default
     namespace comes from the runtime's login: podman's `login --get-login`, or,
     for docker, which has no such option, its client configuration and the
     credential helper it names.
"""

import base64
import os
import pathlib
import shutil
import subprocess
import tempfile
import unittest

SCRIPTS_DIR = pathlib.Path(__file__).resolve().parent
SCRIPT_NAME = "build-adapter-images.sh"
LIBRARIES = ["dev-runtime.sh", "adapter-build.sh"]

CURATED_ORDER = [
    "inspect",
    "lm-evaluation-harness",
    "lighteval",
    "ruler",
    "ifbench",
    "followbench",
]

# The image name each adapter is pushed as, mirrored from lib/adapter-build.sh:
# the tests assert the registry destination, not the name mapping itself.
IMAGE_NAMES = {
    "inspect": "community-inspect",
    "lm-evaluation-harness": "community-lmes",
    "lighteval": "community-lighteval",
    "ruler": "community-ruler",
    "ifbench": "community-ifbench",
    "followbench": "community-followbench",
}

CONTRIB_ADAPTERS = [adapter for adapter in CURATED_ORDER if adapter != "lm-evaluation-harness"]

# The stub records every call as "<engine> <args>", so a case can assert which
# engine ran, what was built, for which platform, and whether anything was
# pulled. Everything the script asks about is answerable from the environment.
RUNTIME_STUB = """#!/usr/bin/env bash
engine="$(basename "$0")"
printf '%s %s\\n' "$engine" "$*" >>"${FAKE_RUNTIME_LOG}"
case "${1:-}" in
  --version) printf '%s version 27.0.0\\n' "$engine"; exit 0 ;;
  info)
    if [[ "${2:-}" == --format ]]; then
      case "${3:-}" in
        '{{.Host.Arch}}') [[ -n "${FAKE_HOST_ARCH:-}" ]] && printf '%s\\n' "$FAKE_HOST_ARCH" ;;
        '{{.Architecture}}') [[ -n "${FAKE_ENGINE_ARCH:-}" ]] && printf '%s\\n' "$FAKE_ENGINE_ARCH" ;;
      esac
    fi
    exit 0
    ;;
  login)
    if [[ -n "${FAKE_LOGIN:-}" ]]; then
      printf '%s\\n' "$FAKE_LOGIN"
      exit 0
    fi
    printf 'Error: not logged into quay.io\\n' >&2
    exit 1
    ;;
  build)
    # The engine writes its progress to stdout, which the script must keep out of
    # the captured ADAPTER_IMAGES value.
    printf 'STEP 1/2: FROM scratch\\n'
    exit "${FAKE_BUILD_EXIT:-0}"
    ;;
  image)
    case "${2:-}" in
      push)
        printf 'Getting image source signatures\\n'
        printf 'Copying blob sha256:0123456789ab done\\n'
        exit "${FAKE_PUSH_EXIT:-0}"
        ;;
      *) exit 0 ;;
    esac
    ;;
esac
exit 1
"""


class PushWorld:
    """A throw-away checkout tree holding the script, its libraries, and sources."""

    def __init__(self, checkouts=True):
        self.root = pathlib.Path(tempfile.mkdtemp())
        self.repo = self.root / "eval-hub"
        (self.repo / "scripts" / "lib").mkdir(parents=True)
        shutil.copy2(SCRIPTS_DIR / SCRIPT_NAME, self.repo / "scripts" / SCRIPT_NAME)
        for library in LIBRARIES:
            shutil.copy2(SCRIPTS_DIR / "lib" / library, self.repo / "scripts" / "lib" / library)
        self.contrib = self.root / "eval-hub-contrib"
        self.lmes = self.root / "lm-evaluation-harness"
        if checkouts:
            self.add_checkouts()
        self.bin = self.root / "bin"
        self.bin.mkdir()
        for engine in ("podman", "docker"):
            path = self.bin / engine
            path.write_text(RUNTIME_STUB)
            path.chmod(0o755)
        self.log = self.root / "runtime.log"

    def add_checkouts(self):
        for adapter in CONTRIB_ADAPTERS:
            directory = self.contrib / "adapters" / adapter
            directory.mkdir(parents=True, exist_ok=True)
            (directory / "Containerfile").write_text("FROM scratch\n")
        self.lmes.mkdir(parents=True, exist_ok=True)
        (self.lmes / "Dockerfile.lmes-job").write_text("FROM scratch\n")

    def containerfile(self, adapter):
        if adapter == "lm-evaluation-harness":
            return self.lmes / "Dockerfile.lmes-job"
        return self.contrib / "adapters" / adapter / "Containerfile"

    def context(self, adapter):
        if adapter == "lm-evaluation-harness":
            return self.lmes
        return self.contrib / "adapters" / adapter

    def write_docker_config(self, content):
        """Write a Docker client configuration and return its DOCKER_CONFIG dir."""
        directory = self.root / "docker"
        directory.mkdir(exist_ok=True)
        (directory / "config.json").write_text(content)
        return str(directory)

    def add_credential_helper(self, name, output):
        """Put a stub docker-credential-<name> helper on the stub PATH."""
        path = self.bin / f"docker-credential-{name}"
        path.write_text(f"#!/usr/bin/env bash\nprintf '%s\\n' '{output}'\n")
        path.chmod(0o755)

    def calls(self):
        """Return the recorded runtime calls as a list of argument lists."""
        if not self.log.exists():
            return []
        return [line.split() for line in self.log.read_text().splitlines() if line]

    def cleanup(self):
        shutil.rmtree(self.root, ignore_errors=True)


class BuildAdapterImagesTestCase(unittest.TestCase):
    """Runs the real script against a stub runtime in a throw-away checkout tree."""

    def setUp(self):
        self.world = PushWorld()

    def tearDown(self):
        self.world.cleanup()

    def run_script(self, adapters="followbench", registry="quay.io/tester", tag="latest", shell="bash",
                   **environment):
        """Run the script once; returns (returncode, stdout, stderr)."""
        env = {
            # The stub engines come first so the run never reaches a real
            # runtime, while `bash` still resolves to the developer's, which is
            # the interpreter `#!/usr/bin/env bash` selects outside the tests.
            "PATH": f"{self.world.bin}:{os.environ['PATH']}",
            "HOME": str(self.world.root),
            "ADAPTERS": adapters,
            "ADAPTER_IMAGE_TAG": tag,
            "FAKE_RUNTIME_LOG": str(self.world.log),
            "FAKE_HOST_ARCH": "arm64",
        }
        if registry is not None:
            env["ADAPTER_IMAGE_REGISTRY"] = registry
        for key, value in environment.items():
            if value is None:
                env.pop(key, None)
            else:
                env[key] = value
        proc = subprocess.run(
            [shell, str(self.world.repo / "scripts" / SCRIPT_NAME)],
            capture_output=True,
            text=True,
            env=env,
        )
        return proc.returncode, proc.stdout, proc.stderr

    def assert_builds(self, calls, adapter, platform, reference, build_args=()):
        """Assert one call built `adapter` for `platform` and tagged `reference`."""
        expected = ["build", "--platform", f"linux/{platform}"]
        for value in build_args:
            expected += ["--build-arg", value]
        expected += [
            "--tag",
            reference,
            "--file",
            str(self.world.containerfile(adapter)),
            str(self.world.context(adapter)),
        ]
        self.assertIn(expected, [call[1:] for call in calls], calls)

    def assert_pushes(self, calls, reference):
        """Assert the build was followed by a push of `reference`."""
        self.assertIn(["image", "push", reference], [call[1:] for call in calls], calls)

    def assert_no_build(self, calls):
        """Assert the run never reached the runtime's build or push commands."""
        self.assertEqual([], [call for call in calls if call[1:2] == ["build"]], calls)
        self.assertEqual([], [call for call in calls if call[1:3] == ["image", "push"]], calls)


class DestinationTests(BuildAdapterImagesTestCase):
    """The pushed reference is the registry namespace plus the shared image name."""

    def test_default_selection_builds_and_pushes_followbench(self):
        code, stdout, stderr = self.run_script()
        self.assertEqual(0, code, stderr)
        reference = "quay.io/tester/community-followbench:latest"
        self.assert_builds(self.world.calls(), "followbench", "arm64", reference)
        self.assert_pushes(self.world.calls(), reference)
        self.assertEqual(f"followbench={reference}\n", stdout)

    def test_all_pushes_the_curated_set_in_order(self):
        code, stdout, stderr = self.run_script(adapters="all")
        self.assertEqual(0, code, stderr)
        expected = ",".join(
            f"{adapter}=quay.io/tester/{IMAGE_NAMES[adapter]}:latest" for adapter in CURATED_ORDER
        )
        self.assertEqual(f"{expected}\n", stdout)
        pushed = [call[3] for call in self.world.calls() if call[1:3] == ["image", "push"]]
        self.assertEqual([f"quay.io/tester/{IMAGE_NAMES[a]}:latest" for a in CURATED_ORDER], pushed)

    def test_lmes_keeps_its_own_image_name_and_target_args(self):
        code, stdout, stderr = self.run_script(adapters="lm-evaluation-harness")
        self.assertEqual(0, code, stderr)
        reference = "quay.io/tester/community-lmes:latest"
        self.assert_builds(
            self.world.calls(),
            "lm-evaluation-harness",
            "arm64",
            reference,
            build_args=("TARGETOS=linux", "TARGETARCH=arm64"),
        )
        self.assert_pushes(self.world.calls(), reference)
        self.assertEqual(f"lm-evaluation-harness={reference}\n", stdout)

    def test_tag_is_applied_to_every_pushed_image(self):
        code, stdout, stderr = self.run_script(adapters="inspect,ruler", tag="v1.2.3")
        self.assertEqual(0, code, stderr)
        self.assertIn("inspect=quay.io/tester/community-inspect:v1.2.3", stdout)
        self.assertIn("ruler=quay.io/tester/community-ruler:v1.2.3", stdout)

    def test_registry_defaults_to_the_runtime_login(self):
        code, stdout, stderr = self.run_script(registry=None, FAKE_LOGIN="someone")
        self.assertEqual(0, code, stderr)
        self.assertEqual("followbench=quay.io/someone/community-followbench:latest\n", stdout)
        self.assertIn("quay.io/someone", stderr)
        self.assertIn(
            ["login", "--get-login", "quay.io"],
            [call[1:] for call in self.world.calls()],
            "podman's account lookup must still use --get-login",
        )

    def test_a_registry_and_namespace_are_both_accepted(self):
        for registry in ("quay.io/tester", "localhost:5000/tester"):
            with self.subTest(registry=registry):
                code, stdout, stderr = self.run_script(registry=registry)
                self.assertEqual(0, code, stderr)
                self.assertEqual(
                    f"followbench={registry}/community-followbench:latest\n", stdout
                )

    def test_explicit_runtime_is_used(self):
        code, _, stderr = self.run_script(
            CONTAINER_RUNTIME="docker", FAKE_HOST_ARCH=None, FAKE_ENGINE_ARCH="arm64"
        )
        self.assertEqual(0, code, stderr)
        self.assertEqual({"docker"}, {call[0] for call in self.world.calls()})
        self.assert_builds(
            self.world.calls(), "followbench", "arm64",
            "quay.io/tester/community-followbench:latest",
        )


class DockerAccountTests(BuildAdapterImagesTestCase):
    """Docker has no `login --get-login`, so its account comes from its config."""

    def test_auths_entry_supplies_the_default_namespace(self):
        encoded = base64.b64encode(b"tester:secret").decode()
        docker_config = self.world.write_docker_config(
            '{"auths": {"quay.io": {"auth": "%s"}}}' % encoded
        )
        code, stdout, stderr = self.run_script(
            registry=None, CONTAINER_RUNTIME="docker", DOCKER_CONFIG=docker_config
        )
        self.assertEqual(0, code, stderr)
        self.assertEqual("followbench=quay.io/tester/community-followbench:latest\n", stdout)
        self.assertNotIn(
            "login",
            [argument for call in self.world.calls() for argument in call[1:]],
            "docker must not be asked for --get-login, which it does not support",
        )

    def test_an_https_prefixed_auths_entry_is_accepted(self):
        encoded = base64.b64encode(b"httpsuser:secret").decode()
        docker_config = self.world.write_docker_config(
            '{"auths": {"https://quay.io": {"auth": "%s"}}}' % encoded
        )
        code, stdout, stderr = self.run_script(
            registry=None, CONTAINER_RUNTIME="docker", DOCKER_CONFIG=docker_config
        )
        self.assertEqual(0, code, stderr)
        self.assertEqual(
            "followbench=quay.io/httpsuser/community-followbench:latest\n", stdout
        )

    def test_a_credential_helper_supplies_the_default_namespace(self):
        self.world.add_credential_helper("stub", '{"quay.io":"helperuser"}')
        docker_config = self.world.write_docker_config(
            '{"auths": {"quay.io": {}}, "credsStore": "stub"}'
        )
        code, stdout, stderr = self.run_script(
            registry=None, CONTAINER_RUNTIME="docker", DOCKER_CONFIG=docker_config
        )
        self.assertEqual(0, code, stderr)
        self.assertEqual(
            "followbench=quay.io/helperuser/community-followbench:latest\n", stdout
        )

    def test_a_per_registry_credential_helper_is_used(self):
        self.world.add_credential_helper("stub", '{"quay.io":"helperuser"}')
        docker_config = self.world.write_docker_config(
            '{"auths": {}, "credHelpers": {"quay.io": "stub"}}'
        )
        code, stdout, stderr = self.run_script(
            registry=None, CONTAINER_RUNTIME="docker", DOCKER_CONFIG=docker_config
        )
        self.assertEqual(0, code, stderr)
        self.assertEqual(
            "followbench=quay.io/helperuser/community-followbench:latest\n", stdout
        )

    def test_no_recorded_login_fails_with_a_docker_specific_diagnostic(self):
        docker_config = self.world.write_docker_config("{}")
        code, stdout, stderr = self.run_script(
            registry=None, CONTAINER_RUNTIME="docker", DOCKER_CONFIG=docker_config
        )
        self.assertNotEqual(0, code)
        self.assertEqual("", stdout)
        self.assertIn("ADAPTER_IMAGE_REGISTRY is not set", stderr)
        self.assertIn("docker login quay.io", stderr)
        self.assertIn("no --get-login option", stderr)
        self.assertIn("config.json", stderr)
        self.assert_no_build(self.world.calls())


class PlatformTests(BuildAdapterImagesTestCase):
    """The build target is the runtime's architecture, never the host's."""

    def test_runtime_architecture_wins_over_the_host(self):
        code, _, stderr = self.run_script(FAKE_HOST_ARCH="amd64")
        self.assertEqual(0, code, stderr)
        self.assert_builds(
            self.world.calls(), "followbench", "amd64",
            "quay.io/tester/community-followbench:latest",
        )

    def test_engine_architecture_is_the_fallback_probe(self):
        code, _, stderr = self.run_script(FAKE_HOST_ARCH=None, FAKE_ENGINE_ARCH="aarch64")
        self.assertEqual(0, code, stderr)
        self.assert_builds(
            self.world.calls(), "followbench", "arm64",
            "quay.io/tester/community-followbench:latest",
        )


class RejectionTests(BuildAdapterImagesTestCase):
    """Inputs that cannot produce a usable push fail before the first build."""

    def test_missing_registry_without_a_login_fails(self):
        code, stdout, stderr = self.run_script(registry=None)
        self.assertNotEqual(0, code)
        self.assertIn("ADAPTER_IMAGE_REGISTRY is not set", stderr)
        self.assertIn("login quay.io", stderr)
        self.assertEqual("", stdout)
        self.assert_no_build(self.world.calls())

    def test_unusable_registry_is_rejected_before_building(self):
        for registry in (
            "quay.io/tester:latest",
            "quay.io/Tester",
            "quay.io/org/team",
            "quay.io/",
            "quay.io/tester@sha256:abc",
            # A registry alone and a namespace alone are both incomplete: the
            # destination must name the registry to push to and the namespace
            # under it.
            "quay.io",
            "localhost:5000",
            "tester",
        ):
            with self.subTest(registry=registry):
                code, stdout, stderr = self.run_script(registry=registry)
                self.assertNotEqual(0, code, f"{registry} was accepted")
                self.assertIn("ADAPTER_IMAGE_REGISTRY must be", stderr)
                self.assertEqual("", stdout)
        self.assert_no_build(self.world.calls())

    def test_unsupported_adapter_is_rejected(self):
        for adapters in ("garak", "inspect,garak-kfp"):
            with self.subTest(adapters=adapters):
                code, stdout, stderr = self.run_script(adapters=adapters)
                self.assertNotEqual(0, code)
                self.assertIn("Unsupported adapter", stderr)
                self.assertIn("lm-evaluation-harness", stderr)
                self.assertEqual("", stdout)
        self.assert_no_build(self.world.calls())

    def test_duplicate_adapter_is_rejected(self):
        code, _, stderr = self.run_script(adapters="inspect, inspect")
        self.assertNotEqual(0, code)
        self.assertIn("duplicate entry", stderr)
        self.assert_no_build(self.world.calls())

    def test_missing_checkout_names_the_searched_path(self):
        self.world.cleanup()
        self.world = PushWorld(checkouts=False)
        code, stdout, stderr = self.run_script(adapters="inspect")
        self.assertNotEqual(0, code)
        self.assertIn("Could not find repository 'eval-hub-contrib'", stderr)
        self.assertIn(str(self.world.root / "eval-hub-contrib"), stderr)
        self.assertEqual("", stdout)
        self.assert_no_build(self.world.calls())

    def test_push_failure_fails_the_run_with_a_login_hint(self):
        code, _, stderr = self.run_script(FAKE_PUSH_EXIT="1")
        self.assertNotEqual(0, code)
        self.assertIn("Could not push quay.io/tester/community-followbench:latest", stderr)
        self.assertIn("login quay.io", stderr)


class OutputTests(BuildAdapterImagesTestCase):
    """Only the value reaches stdout, and nothing is ever pulled."""

    def test_stdout_is_one_line_and_diagnostics_are_on_stderr(self):
        code, stdout, stderr = self.run_script(adapters="inspect,ifbench")
        self.assertEqual(0, code, stderr)
        self.assertEqual(1, len(stdout.splitlines()))
        self.assertNotIn("[INFO]", stdout)
        self.assertIn("[INFO]", stderr)

    def test_runtime_progress_stays_out_of_the_captured_value(self):
        """The engine writes build and push progress to stdout of its own."""
        code, stdout, stderr = self.run_script(adapters="inspect")
        self.assertEqual(0, code, stderr)
        self.assertEqual("inspect=quay.io/tester/community-inspect:latest\n", stdout)
        self.assertIn("STEP 1/2: FROM scratch", stderr)
        self.assertIn("Getting image source signatures", stderr)
        self.assertNotIn("STEP 1/2", stdout)
        self.assertNotIn("Copying blob", stdout)

    def test_nothing_is_pulled(self):
        code, _, stderr = self.run_script(adapters="all")
        self.assertEqual(0, code, stderr)
        self.assertEqual([], [call for call in self.world.calls() if call[1:3] == ["image", "pull"]])


class PortabilityTests(BuildAdapterImagesTestCase):
    """The script runs under the stock macOS /bin/bash, which is still 3.2.

    Pushing from a Mac is the point of this target, so a fresh machine only has
    /bin/bash. bash 3.2 treats an empty "${array[@]}" as an unbound variable
    under `set -u`, which is why the selection check is indexed.
    """

    def test_runs_under_system_bash(self):
        system_bash = pathlib.Path("/bin/bash")
        if not system_bash.exists():
            self.skipTest("/bin/bash is not available")
        code, stdout, stderr = self.run_script(shell=str(system_bash))
        self.assertEqual(0, code, stderr)
        self.assertEqual(
            "followbench=quay.io/tester/community-followbench:latest\n", stdout
        )


if __name__ == "__main__":
    unittest.main()
