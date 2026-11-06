"""Regression tests for how the dev scripts resolve the container runtime.

dev-deploy records the engine that backs a kind cluster next to the private
kubeconfig, and dev-deploy, dev-clean, dev-demo, and clean-container-storage
resolve the runtime from that record instead of probing both engines. These tests
extract the real function bodies from each script, stub podman, docker, and kind
on PATH, and check the resolution order:

  1. an explicit CONTAINER_RUNTIME, which must match the recorded engine for the
     scripts that build, push, or delete;
  2. the recorded engine;
  3. the engine that owns KIND_CLUSTER_NAME (clusters deployed before the record);
  4. the first reachable engine (dev-deploy and clean-container-storage), the
     owning engine then docker (dev-demo), and podman before docker.

The podman-docker shim cases check that a `docker` that is really podman is
resolved to podman, so kind uses the podman provider and registry pushes keep
`--tls-verify=false`.

Run from the repository root with `make test-scripts`, or directly:

    python3 -m unittest discover -s scripts
"""

import os
import pathlib
import re
import subprocess
import tempfile
import unittest

SCRIPTS_DIR = pathlib.Path(__file__).resolve().parent

# The functions each script needs for its runtime resolution, extracted from the
# script itself so the tests cannot drift from the implementation.
SCRIPT_FUNCTIONS = {
    "dev-deploy.sh": [
        "log",
        "fail",
        "need_command",
        "kind_cluster_in_runtime",
        "canonical_runtime",
        "recorded_runtime",
        "use_runtime",
        "detect_container_runtime",
    ],
    "dev-clean.sh": [
        "log",
        "fail",
        "kind_cluster_in_runtime",
        "canonical_runtime",
        "recorded_runtime",
        "use_runtime",
        "detect_container_runtime",
    ],
    "dev-demo.sh": [
        "log",
        "fail",
        "need_command",
        "kind_cluster_in_runtime",
        "recorded_runtime",
        "detect_container_runtime",
    ],
    "clean-container-storage.sh": [
        "log",
        "fail",
        "kind_cluster_in_runtime",
        "recorded_runtime",
        "detect_container_runtime",
    ],
}

CLUSTER = "probe-cluster"

PODMAN_FAKE = """#!/usr/bin/env bash
case "${1:-}" in
  info) [[ "${FAKE_PODMAN_REACHABLE:-1}" == 1 ]] && exit 0 || exit 1 ;;
  --version|-v) echo "podman version 5.0.0" ;;
esac
exit 0
"""

DOCKER_FAKE = """#!/usr/bin/env bash
case "${1:-}" in
  info) [[ "${FAKE_DOCKER_REACHABLE:-1}" == 1 ]] && exit 0 || exit 1 ;;
  --version) echo "${FAKE_DOCKER_VERSION:-Docker version 27.0.3, build test}" ;;
esac
exit 0
"""

# kind lists the clusters of the engine it is pointed at, which is what makes
# "the other engine owns this name" invisible to a probe under the wrong engine.
KIND_FAKE = """#!/usr/bin/env bash
if [[ "${1:-}" == get && "${2:-}" == clusters ]]; then
  if [[ -n "${KIND_EXPERIMENTAL_PROVIDER:-}" ]]; then
    printf '%s' "${FAKE_PODMAN_CLUSTERS:-}"
  else
    printf '%s' "${FAKE_DOCKER_CLUSTERS:-}"
  fi
fi
exit 0
"""


def extract_functions(script, names):
    """Return the source of `names` from `script`, comments included."""
    lines = (SCRIPTS_DIR / script).read_text().splitlines()
    blocks = []
    for name in names:
        start = next((i for i, line in enumerate(lines) if re.match(rf"^{name}\(\) \{{", line)), None)
        assert start is not None, f"{name}() not found in scripts/{script}"
        while start > 0 and lines[start - 1].startswith("#"):
            start -= 1
        end = start
        while not lines[end].startswith("}"):
            end += 1
        blocks.extend(lines[start:end + 1])
        blocks.append("")
    return "\n".join(blocks)


class RuntimeResolutionTestCase(unittest.TestCase):
    """Runs each script's detect_container_runtime against stubbed engines."""

    @classmethod
    def setUpClass(cls):
        cls.work = tempfile.TemporaryDirectory()
        cls.work_dir = pathlib.Path(cls.work.name)
        cls.bin_dir = cls.work_dir / "bin"
        cls.bin_dir.mkdir()
        for name, body in (("podman", PODMAN_FAKE), ("docker", DOCKER_FAKE), ("kind", KIND_FAKE)):
            stub = cls.bin_dir / name
            stub.write_text(body)
            stub.chmod(0o755)

    @classmethod
    def tearDownClass(cls):
        cls.work.cleanup()

    def resolve(self, script, explicit="", recorded="", podman_clusters="", docker_clusters="", shim=False, podman_reachable=True):
        """Return (returncode, resolved runtime, output) for one scenario."""
        state_path = self.work_dir / f"{CLUSTER}.runtime"
        if state_path.exists():
            state_path.unlink()
        if recorded:
            state_path.write_text(recorded + "\n")
        harness = self.work_dir / "case.sh"
        harness.write_text(
            "#!/usr/bin/env bash\n"
            "set -Eeuo pipefail\n"
            f'CONTAINER_RUNTIME="{explicit}"\n'
            f"KIND_CLUSTER_NAME={CLUSTER}\n"
            f'KUBECONFIG_DIR="{self.work_dir}"\n'
            f'RUNTIME_STATE_FILE="{state_path}"\n'
            f"{extract_functions(script, SCRIPT_FUNCTIONS[script])}\n"
            "detect_container_runtime\n"
            'printf "RESULT=%s\\n" "$CONTAINER_RUNTIME"\n'
        )
        env = {
            "PATH": f"{self.bin_dir}:{os.environ['PATH']}",
            "HOME": str(self.work_dir),
            "FAKE_PODMAN_CLUSTERS": podman_clusters,
            "FAKE_DOCKER_CLUSTERS": docker_clusters,
            "FAKE_PODMAN_REACHABLE": "1" if podman_reachable else "0",
            "FAKE_DOCKER_VERSION": (
                "podman version 5.0.0 (podman-docker shim)"
                if shim
                else "Docker version 27.0.3, build test"
            ),
        }
        proc = subprocess.run(["bash", str(harness)], capture_output=True, text=True, env=env)
        resolved = ""
        for line in proc.stdout.splitlines():
            if line.startswith("RESULT="):
                resolved = line.split("=", 1)[1]
        return proc.returncode, resolved, proc.stdout + proc.stderr

    def assert_resolves(self, script, expected, **kwargs):
        """Assert that `script` resolves `expected` for the given scenario."""
        code, resolved, output = self.resolve(script, **kwargs)
        self.assertEqual(code, 0, output)
        self.assertEqual(resolved, expected, output)
        return output

class DeployResolutionTests(RuntimeResolutionTestCase):
    def test_explicit_runtime_is_used(self):
        """An explicit CONTAINER_RUNTIME with no record is used as given."""
        self.assert_resolves("dev-deploy.sh", "podman", explicit="podman")

    def test_recorded_runtime_wins_over_the_other_engine(self):
        """The record decides, even when the other engine owns a same-named cluster."""
        self.assert_resolves(
            "dev-deploy.sh", "podman", recorded="podman", docker_clusters=CLUSTER + "\n"
        )

    def test_unrecorded_cluster_selects_its_owning_engine(self):
        """A cluster deployed before the record existed is still reused."""
        self.assert_resolves("dev-deploy.sh", "docker", docker_clusters=CLUSTER + "\n")

    def test_explicit_runtime_must_match_the_record(self):
        """A mismatch fails instead of creating a duplicate cluster and kubeconfig."""
        code, _, output = self.resolve("dev-deploy.sh", explicit="docker", recorded="podman")
        self.assertNotEqual(code, 0, output)
        self.assertIn("was deployed with podman", output)

    def test_podman_docker_shim_resolves_to_podman(self):
        """The shim is podman, so kind and the registry push must use podman."""
        self.assert_resolves("dev-deploy.sh", "podman", explicit="docker", shim=True)
        self.assert_resolves("dev-deploy.sh", "podman", recorded="docker", shim=True)

    def test_first_reachable_engine_is_used(self):
        """With nothing recorded and no cluster, podman is preferred."""
        self.assert_resolves("dev-deploy.sh", "podman")
        self.assert_resolves("dev-deploy.sh", "docker", podman_reachable=False)

class CleanResolutionTests(RuntimeResolutionTestCase):
    def test_recorded_runtime_is_reused(self):
        """`make dev-clean` after `make dev-deploy` needs no second input."""
        self.assert_resolves("dev-clean.sh", "podman", recorded="podman")

    def test_explicit_runtime_must_match_the_record(self):
        """Cleaning the other engine would skip the cluster and still drop its kubeconfig."""
        code, _, output = self.resolve("dev-clean.sh", explicit="docker", recorded="podman")
        self.assertNotEqual(code, 0, output)
        self.assertIn("was deployed with podman", output)

    def test_unrecorded_cluster_selects_its_owning_engine(self):
        """Cleanup of a pre-record deployment still targets its engine."""
        self.assert_resolves("dev-clean.sh", "docker", docker_clusters=CLUSTER + "\n")

    def test_podman_docker_shim_resolves_to_podman(self):
        """The shim is podman, which owns the registry container and image tags."""
        self.assert_resolves("dev-clean.sh", "podman", explicit="docker", shim=True)
        self.assert_resolves("dev-clean.sh", "podman", recorded="docker", shim=True)

    def test_leftover_registry_is_still_cleaned(self):
        """With no record and no cluster the first reachable engine is used."""
        self.assert_resolves("dev-clean.sh", "podman")
        self.assert_resolves("dev-clean.sh", "docker", podman_reachable=False)

class DemoResolutionTests(RuntimeResolutionTestCase):
    def test_recorded_runtime_is_reused(self):
        """dev-demo queries kind with the engine that backed the deployment."""
        self.assert_resolves("dev-demo.sh", "podman", recorded="podman")

    def test_explicit_runtime_does_not_contradict_the_record(self):
        """dev-demo queries the recorded engine and warns instead of failing."""
        output = self.assert_resolves("dev-demo.sh", "podman", explicit="docker", recorded="podman")
        self.assertIn("was deployed with podman", output)

    def test_unrecorded_cluster_selects_its_owning_engine(self):
        """A cluster deployed before the record existed is still queried."""
        self.assert_resolves("dev-demo.sh", "docker", docker_clusters=CLUSTER + "\n")

    def test_missing_cluster_still_queries_docker(self):
        """The docker fallback lets check_cluster report the missing cluster."""
        self.assert_resolves("dev-demo.sh", "docker")


class StorageCleanupResolutionTests(RuntimeResolutionTestCase):
    def test_recorded_runtime_is_reused(self):
        """Pruning targets the engine that backs the dev stack."""
        self.assert_resolves("clean-container-storage.sh", "podman", recorded="podman")

    def test_unrecorded_cluster_selects_its_owning_engine(self):
        """A cluster deployed before the record existed still selects its engine."""
        self.assert_resolves("clean-container-storage.sh", "docker", docker_clusters=CLUSTER + "\n")

    def test_first_reachable_engine_is_used(self):
        """With nothing recorded and no cluster, podman is preferred."""
        self.assert_resolves("clean-container-storage.sh", "podman")
        self.assert_resolves("clean-container-storage.sh", "docker", podman_reachable=False)


if __name__ == "__main__":
    unittest.main()

class RecordRoundTripTests(RuntimeResolutionTestCase):
    def test_recorded_engine_round_trips(self):
        """The record dev-deploy writes is what the other scripts read back."""
        state_path = self.work_dir / f"{CLUSTER}.runtime"
        if state_path.exists():
            state_path.unlink()
        harness = self.work_dir / "record.sh"
        harness.write_text(
            "#!/usr/bin/env bash\n"
            "set -Eeuo pipefail\n"
            "umask 077\n"
            'CONTAINER_RUNTIME="docker"\n'
            f"KIND_CLUSTER_NAME={CLUSTER}\n"
            f'RUNTIME_STATE_FILE="{state_path}"\n'
            f"{extract_functions('dev-deploy.sh', ['log', 'fail', 'recorded_runtime', 'record_runtime'])}\n"
            "record_runtime\n"
            'printf "READ=%s\\n" "$(recorded_runtime)"\n'
        )
        proc = subprocess.run(
            ["bash", str(harness)],
            capture_output=True,
            text=True,
            env={"PATH": os.environ["PATH"], "HOME": str(self.work_dir)},
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("READ=docker", proc.stdout)
        self.assertEqual(state_path.read_text(), "docker\n")
        self.assertEqual(oct(state_path.stat().st_mode & 0o777), "0o600")
