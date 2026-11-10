"""Regression tests for how the dev scripts resolve the container runtime.

dev-deploy records the engine that backs a kind cluster next to the private
kubeconfig, and dev-deploy, dev-clean, dev-demo, and clean-container-storage
resolve the runtime from that record instead of probing both engines. These tests
extract the real function bodies from each script, stub podman, docker, and kind
on PATH, and check the resolution order:

  1. an explicit CONTAINER_RUNTIME: dev-deploy and dev-clean reject a value that
     does not match the record, and one that does not own an unrecorded cluster,
     while clean-container-storage warns when it differs from the record and
     keeps the explicit engine;
  2. the recorded engine, which dev-deploy and dev-demo use only while it still
     runs the cluster (see the stale-record cases);
  3. the engine that owns KIND_CLUSTER_NAME (clusters deployed before the record);
  4. the first reachable engine (dev-deploy and clean-container-storage), the
     owning engine then docker (dev-demo), and podman before docker, or docker
     before podman under WSL.

A record is only an answer to "which engine backed this cluster", not proof that
it still does: the stale-record cases check that dev-deploy fails instead of
creating a second cluster over one the other engine still runs, that dev-demo
queries the engine that owns the cluster instead, and that dev-clean drops the
record for a cluster that is already gone. The unreachable-record cases check
that a stopped recorded engine is still queried by dev-demo and cannot stop
clean-container-storage from pruning, because a stopped or uninstalled engine
must not make storage unreclaimable.

A record whose engine is no longer installed cannot describe a reachable cluster,
so it must not keep a command from running: the missing-engine cases check that
dev-deploy and dev-demo ignore such a record, that dev-clean drops it (also when
the explicit engine names another one), and that a recorded engine which is
installed but stopped fails with the state file to remove. The explicit cases also
check that an explicit CONTAINER_RUNTIME matching the record still has to own the
cluster, so naming the recorded engine cannot skip that guard.

The podman-docker shim cases check that a `docker` that is really podman is
resolved to podman for every script, so kind uses the podman provider and
registry pushes keep `--tls-verify=false`.

The delete-cluster cases check that `make dev-clean DELETE_CLUSTER=true` removes
the runtime record even when it preserves the private kubeconfig (a kubeconfig
that targets another cluster, or no private kubeconfig at all), because a stale
record rejects a later deploy that explicitly selects another engine. They also
check that the kubeconfig survives when the selected engine no longer runs the
cluster but the other engine still lists it or cannot be queried, and that it is
removed once neither engine holds the name and its API server stops answering.

Run from the repository root with `make test-scripts`, or directly:

    python3 -m unittest discover -s scripts
"""

import os
import pathlib
import re
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

SCRIPTS_DIR = pathlib.Path(__file__).resolve().parent
LIB_PATH = SCRIPTS_DIR / "lib" / "dev-runtime.sh"

# The runtime selection helpers live in scripts/lib/dev-runtime.sh; each script
# keeps only its own detect_container_runtime policy, which is what the
# per-script lists below extract. Both are read from the real files so the tests
# cannot drift from the implementation.
LIB_FUNCTIONS = [
    "kind_cmd",
    "canonical_runtime",
    "kind_cluster_in_runtime",
    "ensure_runtime_owns_existing_cluster",
    "recorded_runtime",
    "record_runtime",
    "runtime_is_installed",
    "runtime_is_reachable",
    "fallback_runtime_order",
    "use_runtime",
]
SCRIPT_FUNCTIONS = {
    "dev-deploy.sh": [
        "log",
        "fail",
        "need_command",
        "detect_container_runtime",
    ],
    "dev-clean.sh": ["log", "fail", "forget_runtime", "detect_container_runtime"],
    "dev-demo.sh": [
        "log",
        "fail",
        "need_command",
        "recorded_runtime_if_trusted",
        "detect_container_runtime",
    ],
    "clean-container-storage.sh": ["log", "fail", "detect_container_runtime"],
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

# The fallback engine order is WSL-aware (WSL2 podman answers 'podman info' but
# kind node startup fails), so the kernel release is stubbed too.
UNAME_FAKE = """#!/usr/bin/env bash
case "${1:-}" in
  -r) printf '%s\\n' "${FAKE_UNAME_RELEASE:-6.8.0-45-generic}" ;;
  *) printf 'Linux\\n' ;;
esac
"""

# dev-clean reads the private kubeconfig's context to decide whether the file is
# worth verifying, and probes the API server before removing it; the delete-cluster
# cases answer the context and make the probe succeed or fail on demand.
KUBECTL_FAKE = """#!/usr/bin/env bash
if [[ "$*" == *"config current-context"* ]]; then
  printf 'kind-%s\\n' "${KIND_CLUSTER_NAME:-}"
elif [[ "$*" == *"--raw /healthz"* ]]; then
  [[ "${FAKE_KUBECTL_HEALTH:-ok}" == ok ]] || exit 1
fi
exit 0
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


class RuntimeResolutionTestCase(unittest.TestCase):
    """Runs each script's detect_container_runtime against stubbed engines."""

    @classmethod
    def setUpClass(cls):
        """Create the stub directory and temporary workspace shared by the class."""
        cls.work = tempfile.TemporaryDirectory()
        cls.work_dir = pathlib.Path(cls.work.name)
        cls.bin_dir = cls.work_dir / "bin"
        cls.bin_dir.mkdir()
        for name, body in (
            ("podman", PODMAN_FAKE),
            ("docker", DOCKER_FAKE),
            ("kind", KIND_FAKE),
            ("kubectl", KUBECTL_FAKE),
            ("uname", UNAME_FAKE),
        ):
            stub = cls.bin_dir / name
            stub.write_text(body)
            stub.chmod(0o755)

    @classmethod
    def tearDownClass(cls):
        """Remove the temporary workspace created by setUpClass."""
        cls.work.cleanup()

    def engine_path(self, missing=()):
        """PATH with the stubs first and no real engine that a case removes.

        A developer machine can have the real podman or docker installed, which
        would answer once the stub is removed, so the directories that hold the
        removed engines are replaced by a mirror of themselves without those
        engines: "not installed" then means what it says. A stub that only
        answers `info` with a failure is a stopped engine, a different state.

        The mirror keeps the rest of the directory reachable. The CI runners keep
        podman, docker, bash, and the coreutils in the same /usr/bin, so dropping
        a directory whole would take the shell that the harness and the stubs
        start with it, and every missing-engine case would error out with
        "bash: No such file or directory" before probing anything.
        """
        if not missing:
            return f"{self.bin_dir}:{os.environ['PATH']}"
        case_bin = self.work_dir / ("bin-no-" + "-".join(sorted(missing)))
        if not case_bin.exists():
            case_bin.mkdir()
            for stub in self.bin_dir.iterdir():
                (case_bin / stub.name).symlink_to(stub)
            for name in missing:
                (case_bin / name).unlink(missing_ok=True)
        dirs = [str(case_bin)]
        for entry in os.environ["PATH"].split(os.pathsep):
            if not entry:
                continue
            hidden = tuple(name for name in missing if (pathlib.Path(entry) / name).exists())
            mapped = self.engine_free_dir(entry, hidden) if hidden else entry
            if mapped not in dirs:
                dirs.append(mapped)
        return os.pathsep.join(dirs)

    def engine_free_dir(self, entry, hidden):
        """Return a mirror of `entry` without the executables named in `hidden`.

        Symlinking the remaining entries keeps a directory that mixes an engine
        with the shell and the coreutils usable, while `command -v` still fails
        for the removed engines. The mirror is built once per directory and
        hidden set, and a directory reached twice through a symlink (`/bin` and
        `/usr/bin` on the CI runners) maps to the same mirror.
        """
        source = pathlib.Path(entry).resolve()
        label = "-".join(("mirror", source.as_posix().strip("/").replace("/", "-"), *hidden))
        mirror = self.work_dir / label
        if not mirror.exists():
            mirror.mkdir()
            for child in source.iterdir():
                if child.name not in hidden:
                    (mirror / child.name).symlink_to(child)
        return str(mirror)

    def resolve(
        self,
        script,
        explicit="",
        recorded="",
        podman_clusters="",
        docker_clusters="",
        shim=False,
        podman_reachable=True,
        docker_reachable=True,
        wsl=False,
        missing=(),
    ):
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
            f"{extract_functions(LIB_PATH, LIB_FUNCTIONS)}\n"
            f"{extract_functions(SCRIPTS_DIR / script, SCRIPT_FUNCTIONS[script])}\n"
            "detect_container_runtime\n"
            'printf "RESULT=%s\\n" "$CONTAINER_RUNTIME"\n'
        )
        env = {
            "PATH": self.engine_path(missing),
            "HOME": str(self.work_dir),
            "FAKE_PODMAN_CLUSTERS": podman_clusters,
            "FAKE_DOCKER_CLUSTERS": docker_clusters,
            "FAKE_PODMAN_REACHABLE": "1" if podman_reachable else "0",
            "FAKE_DOCKER_REACHABLE": "1" if docker_reachable else "0",
            "FAKE_UNAME_RELEASE": (
                "4.4.0-19041-Microsoft" if wsl else "6.8.0-45-generic"
            ),
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


class EnginePathTests(RuntimeResolutionTestCase):
    """The PATH used by the missing-engine cases must hide the engine only."""

    def test_engine_sharing_a_directory_with_the_shell_stays_removed(self):
        """podman and docker sit in /usr/bin next to bash on the CI runners.

        Dropping that directory instead of mirroring it removed the shell with
        the engine, so the harness never started: the missing-engine cases failed
        with "bash: No such file or directory" instead of checking a resolution.
        """
        shared = self.work_dir / "shared-bin"
        shared.mkdir()
        for name in ("bash", "grep"):
            real = shutil.which(name)
            self.assertIsNotNone(real, f"{name} is required by this test")
            (shared / name).symlink_to(real)
        (shared / "podman").symlink_to(shutil.which("bash"))
        with mock.patch.dict(os.environ, {"PATH": str(shared)}):
            path = self.engine_path(missing=("podman",))
        self.assertIsNone(shutil.which("podman", path=path), path)
        for name in ("bash", "grep"):
            found = shutil.which(name, path=path)
            self.assertIsNotNone(found, f"{name} is unreachable through {path}")
            self.assertEqual(pathlib.Path(found).resolve(), (shared / name).resolve(), path)


class DeployResolutionTests(RuntimeResolutionTestCase):
    """dev-deploy must reuse a cluster and refuse to duplicate one."""

    def test_explicit_runtime_is_used(self):
        """An explicit CONTAINER_RUNTIME with no record is used as given."""
        self.assert_resolves("dev-deploy.sh", "podman", explicit="podman")

    def test_recorded_runtime_is_reused(self):
        """The record decides, so a redeploy needs no second input."""
        self.assert_resolves("dev-deploy.sh", "podman", recorded="podman")

    def test_stale_record_does_not_duplicate_the_cluster_the_other_engine_owns(self):
        """A record whose cluster moved to the other engine fails instead of overwriting it."""
        code, _, output = self.resolve(
            "dev-deploy.sh", recorded="podman", docker_clusters=CLUSTER + "\n"
        )
        self.assertNotEqual(code, 0, output)
        self.assertIn("recorded as podman-backed", output)
        self.assertIn("rm -- ", output)

    def test_unrecorded_cluster_selects_its_owning_engine(self):
        """A cluster deployed before the record existed is still reused."""
        self.assert_resolves("dev-deploy.sh", "docker", docker_clusters=CLUSTER + "\n")

    def test_explicit_runtime_must_match_the_record(self):
        """A mismatch fails instead of creating a duplicate cluster and kubeconfig."""
        code, _, output = self.resolve("dev-deploy.sh", explicit="docker", recorded="podman")
        self.assertNotEqual(code, 0, output)
        self.assertIn("was deployed with podman", output)

    def test_explicit_runtime_must_own_an_unrecorded_cluster(self):
        """A cluster deployed before the record existed is not duplicated either."""
        code, _, output = self.resolve(
            "dev-deploy.sh", explicit="docker", podman_clusters=CLUSTER + "\n"
        )
        self.assertNotEqual(code, 0, output)
        self.assertIn("exists under podman, not docker", output)

    def test_explicit_runtime_reuses_the_cluster_it_owns(self):
        """An explicit engine still reuses an unrecorded cluster of its own."""
        self.assert_resolves(
            "dev-deploy.sh", "podman", explicit="podman", podman_clusters=CLUSTER + "\n"
        )

    def test_explicit_runtime_creates_a_cluster_no_engine_owns(self):
        """With no record and no cluster, the explicit engine is used as given."""
        self.assert_resolves("dev-deploy.sh", "docker", explicit="docker")

    def test_podman_docker_shim_resolves_to_podman(self):
        """The shim is podman, so kind and the registry push must use podman."""
        self.assert_resolves("dev-deploy.sh", "podman", explicit="docker", shim=True)
        self.assert_resolves("dev-deploy.sh", "podman", recorded="docker", shim=True)

    def test_first_reachable_engine_is_used(self):
        """With nothing recorded and no cluster, podman is preferred."""
        self.assert_resolves("dev-deploy.sh", "podman")
        self.assert_resolves("dev-deploy.sh", "docker", podman_reachable=False)

    def test_wsl_prefers_docker_on_a_first_deploy(self):
        """WSL2 podman answers 'podman info' but fails kind node startup."""
        self.assert_resolves("dev-deploy.sh", "docker", wsl=True)

    def test_explicit_runtime_matching_the_record_still_owns_the_cluster(self):
        """Naming the recorded engine must not skip the ownership guard."""
        code, _, output = self.resolve(
            "dev-deploy.sh",
            recorded="podman",
            explicit="podman",
            docker_clusters=CLUSTER + "\n",
        )
        self.assertNotEqual(code, 0, output)
        self.assertIn("recorded as podman-backed", output)
        self.assertIn("rm -- ", output)

    def test_uninstalled_recorded_engine_is_ignored(self):
        """A record naming an engine that is gone must not block a deploy."""
        code, resolved, output = self.resolve(
            "dev-deploy.sh", recorded="podman", missing=("podman",)
        )
        self.assertEqual(code, 0, output)
        self.assertEqual(resolved, "docker", output)
        self.assertIn("not installed; ignoring the stale record", output)

    def test_stopped_recorded_engine_names_the_state_file(self):
        """A stopped engine fails with the record to remove, not a bare probe error."""
        code, _, output = self.resolve(
            "dev-deploy.sh", recorded="podman", podman_reachable=False
        )
        self.assertNotEqual(code, 0, output)
        self.assertIn(f"{CLUSTER}.runtime", output)
        self.assertIn("rm -- ", output)


class CleanResolutionTests(RuntimeResolutionTestCase):
    """dev-clean must target the engine that owns the deployment and registry."""

    def test_recorded_runtime_is_reused(self):
        """`make dev-clean` after `make dev-deploy` needs no second input."""
        self.assert_resolves("dev-clean.sh", "podman", recorded="podman")

    def test_explicit_runtime_must_match_the_record(self):
        """Cleaning the other engine would skip the cluster and still drop its kubeconfig."""
        code, _, output = self.resolve("dev-clean.sh", explicit="docker", recorded="podman")
        self.assertNotEqual(code, 0, output)
        self.assertIn("was deployed with podman", output)

    def test_explicit_runtime_must_own_an_unrecorded_cluster(self):
        """Cleaning under the other engine skips the cluster and still reports success."""
        code, _, output = self.resolve(
            "dev-clean.sh", explicit="docker", podman_clusters=CLUSTER + "\n"
        )
        self.assertNotEqual(code, 0, output)
        self.assertIn("exists under podman, not docker", output)
        self.assertIn("CONTAINER_RUNTIME=podman", output)

    def test_explicit_runtime_cleans_the_cluster_it_owns(self):
        """An explicit engine that owns the cluster is not rejected by the guard."""
        self.assert_resolves(
            "dev-clean.sh", "podman", explicit="podman", podman_clusters=CLUSTER + "\n"
        )

    def test_explicit_runtime_cleans_leftovers_no_engine_owns(self):
        """With no cluster under either engine, leftover registry state is still removed."""
        self.assert_resolves("dev-clean.sh", "docker", explicit="docker")

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

    def test_wsl_prefers_docker_on_a_first_clean(self):
        """The WSL engine order is shared with dev-deploy."""
        self.assert_resolves("dev-clean.sh", "docker", wsl=True)

    def test_uninstalled_recorded_engine_is_dropped(self):
        """A record naming an engine that is gone must not block cleanup."""
        code, resolved, output = self.resolve(
            "dev-clean.sh", recorded="podman", missing=("podman",)
        )
        self.assertEqual(code, 0, output)
        self.assertEqual(resolved, "docker", output)
        self.assertIn("dropping the stale record", output)
        self.assertFalse((self.work_dir / f"{CLUSTER}.runtime").exists(), output)

    def test_uninstalled_record_does_not_reject_the_explicit_engine(self):
        """Cleaning with the explicit engine must not fail on a record it cannot honour."""
        code, resolved, output = self.resolve(
            "dev-clean.sh", recorded="podman", explicit="docker", missing=("podman",)
        )
        self.assertEqual(code, 0, output)
        self.assertEqual(resolved, "docker", output)

    def test_stopped_recorded_engine_names_the_state_file(self):
        """A stopped engine fails with the record to remove and keeps it."""
        code, _, output = self.resolve(
            "dev-clean.sh", recorded="podman", podman_reachable=False
        )
        self.assertNotEqual(code, 0, output)
        self.assertIn(f"{CLUSTER}.runtime", output)
        self.assertIn("rm -- ", output)
        self.assertTrue((self.work_dir / f"{CLUSTER}.runtime").exists(), output)


class DemoResolutionTests(RuntimeResolutionTestCase):
    """dev-demo must query kind through the engine that backs the deployment."""

    def test_recorded_runtime_is_reused(self):
        """dev-demo queries kind with the engine that backed the deployment."""
        self.assert_resolves(
            "dev-demo.sh", "podman", recorded="podman", podman_clusters=CLUSTER + "\n"
        )

    def test_explicit_runtime_does_not_contradict_the_record(self):
        """dev-demo queries the recorded engine and warns instead of failing."""
        output = self.assert_resolves(
            "dev-demo.sh",
            "podman",
            explicit="docker",
            recorded="podman",
            podman_clusters=CLUSTER + "\n",
        )
        self.assertIn("was deployed with podman", output)

    def test_stale_record_queries_the_engine_that_owns_the_cluster(self):
        """A record whose engine no longer runs the cluster must not hide the owner."""
        code, resolved, output = self.resolve(
            "dev-demo.sh", recorded="podman", docker_clusters=CLUSTER + "\n"
        )
        self.assertEqual(code, 0, output)
        self.assertEqual(resolved, "docker", output)
        self.assertIn("no longer runs it", output)

    def test_stale_record_does_not_override_the_explicit_engine(self):
        """A stale record cannot send kind to an engine the user did not select."""
        code, resolved, output = self.resolve(
            "dev-demo.sh",
            explicit="docker",
            recorded="podman",
            docker_clusters=CLUSTER + "\n",
        )
        self.assertEqual(code, 0, output)
        self.assertEqual(resolved, "docker", output)
        self.assertIn("no longer runs it", output)

    def test_stopped_recorded_engine_is_still_queried(self):
        """A stopped engine cannot be checked, so its record is the only answer."""
        code, resolved, output = self.resolve(
            "dev-demo.sh", recorded="podman", podman_reachable=False
        )
        self.assertEqual(code, 0, output)
        self.assertEqual(resolved, "podman", output)

    def test_unrecorded_cluster_selects_its_owning_engine(self):
        """A cluster deployed before the record existed is still queried."""
        self.assert_resolves("dev-demo.sh", "docker", docker_clusters=CLUSTER + "\n")

    def test_missing_cluster_still_queries_docker(self):
        """The docker fallback lets check_cluster report the missing cluster."""
        self.assert_resolves("dev-demo.sh", "docker")

    def test_podman_docker_shim_resolves_to_podman(self):
        """A shim `docker` is queried as podman, or kind asks the wrong provider."""
        self.assert_resolves("dev-demo.sh", "podman", explicit="docker", shim=True)
        self.assert_resolves(
            "dev-demo.sh",
            "podman",
            recorded="docker",
            shim=True,
            podman_clusters=CLUSTER + "\n",
        )
        self.assert_resolves("dev-demo.sh", "podman", shim=True)

    def test_recorded_shim_over_an_explicit_shim_still_resolves_to_podman(self):
        """The record is canonicalized too, so the explicit-mismatch branch cannot leave docker."""
        self.assert_resolves(
            "dev-demo.sh",
            "podman",
            explicit="docker",
            recorded="docker",
            shim=True,
            podman_clusters=CLUSTER + "\n",
        )

    def test_uninstalled_recorded_engine_falls_back_to_the_owner(self):
        """A record naming a missing engine must not point kind at it."""
        code, resolved, output = self.resolve(
            "dev-demo.sh", recorded="podman", missing=("podman",)
        )
        self.assertEqual(code, 0, output)
        self.assertEqual(resolved, "docker", output)

    def test_uninstalled_record_does_not_override_the_explicit_engine(self):
        """An explicit engine must not be swapped for a record that names a dead one."""
        code, resolved, output = self.resolve(
            "dev-demo.sh", explicit="podman", recorded="docker", missing=("docker",)
        )
        self.assertEqual(code, 0, output)
        self.assertEqual(resolved, "podman", output)


class DeleteClusterRecordTests(RuntimeResolutionTestCase):
    """`make dev-clean DELETE_CLUSTER=true` must not leave a stale runtime record."""

    def delete_cluster(
        self,
        kubeconfig="owned",
        same_cluster=True,
        cluster_exists=True,
        other_clusters="",
        other_reachable=True,
        health_ok=True,
    ):
        """Run dev-clean's delete_cluster against a stubbed cluster.

        `kubeconfig` is "owned" for a private kubeconfig that targets the cluster
        and "missing" for one that was never created; `same_cluster` decides
        whether that kubeconfig is verified as targeting the deleted cluster.
        `cluster_exists` decides whether the selected engine still runs it;
        `other_clusters`, `other_reachable` and `health_ok` describe the other
        engine and the API server probe for the case where it does not.
        Returns (output, private kubeconfig path, runtime record path) with the
        paths reflecting the state left by the run.
        """
        state_path = self.work_dir / f"{CLUSTER}.runtime"
        state_path.write_text("podman\n")
        private = self.work_dir / f"{CLUSTER}.yaml"
        private.unlink(missing_ok=True)
        if kubeconfig != "missing":
            private.write_text("apiVersion: v1\n")
        exported = self.work_dir / "exported.yaml"
        exported.write_text("apiVersion: v1\n")
        harness = self.work_dir / "delete-cluster.sh"
        harness.write_text(
            "#!/usr/bin/env bash\n"
            "set -Eeuo pipefail\n"
            f"KIND_CLUSTER_NAME={CLUSTER}\n"
            "CONTAINER_RUNTIME=podman\n"
            f'PRIVATE_KUBECONFIG="{private}"\n'
            f'EXPORTED_KUBECONFIG="{exported}"\n'
            f'RUNTIME_STATE_FILE="{state_path}"\n'
            f"{extract_functions(LIB_PATH, ['kind_cluster_in_runtime'])}\n"
            f"{extract_functions(SCRIPTS_DIR / 'dev-clean.sh', ['log', 'fail', 'forget_runtime', 'other_runtime_cluster_state'])}\n"
            f"{extract_functions(SCRIPTS_DIR / 'dev-clean.sh', ['delete_cluster'])}\n"
            "# Stand in for the cluster, kind, kubeconfig export, and registry\n"
            "# cleanup so the run reaches the kubeconfig and record decisions.\n"
            f"cluster_exists() {{ return {0 if cluster_exists else 1}; }}\n"
            "kind_cmd() { :; }\n"
            "export_runtime_kubeconfig() { return 0; }\n"
            f"kubeconfigs_target_same_cluster() {{ return {0 if same_cluster else 1}; }}\n"
            "remove_registry() { :; }\n"
            "delete_cluster\n"
        )
        proc = subprocess.run(
            ["bash", str(harness)],
            capture_output=True,
            text=True,
            env={
                "PATH": f"{self.bin_dir}:{os.environ['PATH']}",
                "HOME": str(self.work_dir),
                "KIND_CLUSTER_NAME": CLUSTER,
                "FAKE_DOCKER_CLUSTERS": other_clusters,
                "FAKE_DOCKER_REACHABLE": "1" if other_reachable else "0",
                "FAKE_KUBECTL_HEALTH": "ok" if health_ok else "unreachable",
            },
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        return proc.stdout + proc.stderr, private, state_path

    def test_record_is_removed_with_the_owned_kubeconfig(self):
        """The kubeconfig that targets the deleted cluster goes with the record."""
        output, private, state_path = self.delete_cluster()
        self.assertFalse(private.exists(), output)
        self.assertFalse(state_path.exists(), output)

    def test_preserved_kubeconfig_still_drops_the_record(self):
        """A kubeconfig kept for another cluster must not keep the stale record."""
        output, private, state_path = self.delete_cluster(same_cluster=False)
        self.assertIn("will be preserved", output)
        self.assertTrue(private.exists(), output)
        self.assertFalse(state_path.exists(), output)

    def test_missing_kubeconfig_still_drops_the_record(self):
        """A deploy without a private kubeconfig still drops the stale record."""
        output, private, state_path = self.delete_cluster(kubeconfig="missing")
        self.assertFalse(private.exists(), output)
        self.assertFalse(state_path.exists(), output)

    def test_cluster_under_the_other_engine_keeps_the_kubeconfig(self):
        """A probe that cannot reach the other engine's cluster proves nothing."""
        output, private, state_path = self.delete_cluster(
            cluster_exists=False, other_clusters=CLUSTER + "\n", health_ok=False
        )
        self.assertIn("still exists under docker", output)
        self.assertTrue(private.exists(), output)
        self.assertFalse(state_path.exists(), output)

    def test_unreachable_other_engine_keeps_the_kubeconfig(self):
        """A stopped engine can still own the cluster, so the kubeconfig stays."""
        output, private, _ = self.delete_cluster(
            cluster_exists=False, other_reachable=False, health_ok=False
        )
        self.assertIn("cannot be checked for that cluster", output)
        self.assertTrue(private.exists(), output)

    def test_gone_from_both_engines_removes_the_kubeconfig(self):
        """With no cluster anywhere and no API server, the kubeconfig goes too."""
        output, private, state_path = self.delete_cluster(cluster_exists=False, health_ok=False)
        self.assertFalse(private.exists(), output)
        self.assertFalse(state_path.exists(), output)


class StaleRecordCleanupTests(RuntimeResolutionTestCase):
    """`make dev-clean` must not leave a record for a cluster that is already gone."""

    def run_main(self, recorded="", cluster_exists=False):
        """Run dev-clean's main with the cluster and the record stubbed.

        `recorded` seeds the runtime record, and `cluster_exists` decides whether
        the selected engine still lists the cluster. Returns the combined output
        and the record path so a case can check what the run left behind.
        """
        state_path = self.work_dir / f"{CLUSTER}.runtime"
        state_path.unlink(missing_ok=True)
        if recorded:
            state_path.write_text(recorded + "\n")
        harness = self.work_dir / "clean-main.sh"
        harness.write_text(
            "#!/usr/bin/env bash\n"
            "set -Eeuo pipefail\n"
            f"KIND_CLUSTER_NAME={CLUSTER}\n"
            'CONTAINER_RUNTIME="podman"\n'
            'DELETE_CLUSTER="false"\n'
            f'RUNTIME_STATE_FILE="{state_path}"\n'
            'WORK_DIR=""\n'
            f"{extract_functions(LIB_PATH, ['recorded_runtime'])}\n"
            f"{extract_functions(SCRIPTS_DIR / 'dev-clean.sh', ['log', 'fail', 'forget_runtime', 'main'])}\n"
            f"cluster_exists() {{ return {0 if cluster_exists else 1}; }}\n"
            "validate_inputs() { :; }\n"
            "check_prerequisites() { :; }\n"
            "check_registry_ownership() { :; }\n"
            "clean_deployment_keep_cluster() { printf 'KEPT_DEPLOYMENT\\n'; }\n"
            "delete_cluster() { printf 'DELETED_CLUSTER\\n'; }\n"
            "remove_registry() { printf 'REMOVED_REGISTRY\\n'; }\n"
            'main "$@"\n'
        )
        proc = subprocess.run(
            ["bash", str(harness)],
            capture_output=True,
            text=True,
            env={"PATH": os.environ["PATH"], "HOME": str(self.work_dir)},
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        return proc.stdout + proc.stderr, state_path

    def test_record_is_dropped_when_the_cluster_is_already_gone(self):
        """A cluster removed out of band must not leave a record that rejects a deploy."""
        output, state_path = self.run_main(recorded="podman")
        self.assertIn("REMOVED_REGISTRY", output)
        self.assertIn("Removed the runtime record", output)
        self.assertFalse(state_path.exists(), output)

    def test_retained_cluster_keeps_the_record(self):
        """A cluster that still exists keeps the record for the next command."""
        output, state_path = self.run_main(recorded="podman", cluster_exists=True)
        self.assertIn("KEPT_DEPLOYMENT", output)
        self.assertTrue(state_path.exists(), output)


class StorageCleanupResolutionTests(RuntimeResolutionTestCase):
    """clean-container-storage must always find an engine to prune."""

    def test_recorded_runtime_is_reused(self):
        """Pruning targets the engine that backs the dev stack."""
        self.assert_resolves("clean-container-storage.sh", "podman", recorded="podman")

    def test_explicit_runtime_warns_instead_of_failing(self):
        """Pruning cannot destroy the cluster, so the explicit engine wins with a warning."""
        output = self.assert_resolves(
            "clean-container-storage.sh", "docker", explicit="docker", recorded="podman"
        )
        self.assertIn("was deployed with podman", output)
        self.assertIn("pruning docker as requested", output)

    def test_unrecorded_cluster_selects_its_owning_engine(self):
        """A cluster deployed before the record existed still selects its engine."""
        self.assert_resolves("clean-container-storage.sh", "docker", docker_clusters=CLUSTER + "\n")

    def test_first_reachable_engine_is_used(self):
        """With nothing recorded and no cluster, podman is preferred."""
        self.assert_resolves("clean-container-storage.sh", "podman")
        self.assert_resolves("clean-container-storage.sh", "docker", podman_reachable=False)

    def test_stopped_record_does_not_override_the_explicit_engine(self):
        """A record naming a stopped engine must not replace the explicit one."""
        output = self.assert_resolves(
            "clean-container-storage.sh",
            "docker",
            explicit="docker",
            recorded="podman",
            podman_reachable=False,
        )
        self.assertIn("pruning docker as requested", output)

    def test_unreachable_record_falls_back_to_the_other_reachable_engine(self):
        """Without an explicit engine the next reachable engine is pruned."""
        output = self.assert_resolves(
            "clean-container-storage.sh",
            "docker",
            recorded="podman",
            podman_reachable=False,
        )
        self.assertIn("selecting another engine to prune", output)

    def test_wsl_prefers_docker_on_a_first_clean(self):
        """The WSL engine order is shared with dev-deploy."""
        self.assert_resolves("clean-container-storage.sh", "docker", wsl=True)


class RecordRoundTripTests(RuntimeResolutionTestCase):
    """The runtime record dev-deploy writes must read back as the same engine."""

    def test_recorded_engine_round_trips(self):
        """The record dev-deploy writes is what the other scripts read back.

        The harness leaves the umask alone, so the 0600 mode has to come from
        record_runtime itself rather than from dev-deploy's `umask 077`: dropping
        that line must not turn the record world-readable.
        """
        state_path = self.work_dir / f"{CLUSTER}.runtime"
        if state_path.exists():
            state_path.unlink()
        harness = self.work_dir / "record.sh"
        harness.write_text(
            "#!/usr/bin/env bash\n"
            "set -Eeuo pipefail\n"
            'CONTAINER_RUNTIME="docker"\n'
            f"KIND_CLUSTER_NAME={CLUSTER}\n"
            f'RUNTIME_STATE_FILE="{state_path}"\n'
            f"{extract_functions(SCRIPTS_DIR / 'dev-deploy.sh', ['log', 'fail'])}\n"
            f"{extract_functions(LIB_PATH, ['recorded_runtime', 'record_runtime'])}\n"
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


if __name__ == "__main__":
    unittest.main()
