"""Regression tests for the govulncheck waiver gate.

``make govulncheck`` cannot silence findings (https://go.dev/issue/61211) and
govulncheck always exits 0 for ``-format=json``, so
scripts/check_govulncheck_waivers.py re-implements the exit status from the JSON
report plus the dated waivers recorded in ``osv-scanner.toml``. These tests pin
that contract: a symbol-level finding fails unless a waiver covers it and has not
expired, package- and module-level findings never fail (matching govulncheck's
text mode), stale waivers are reported, and malformed input is rejected instead
of silently passing.

The fixtures mirror what govulncheck v1.1.4 emits: a stream of concatenated,
indented JSON objects, one finding per call stack, with the vulnerable symbol in
``trace[0]`` and the call site in this module last.
"""

import contextlib
import datetime
import io
import json
import tempfile
import unittest
from pathlib import Path

import check_govulncheck_waivers as gate

TODAY = datetime.date(2026, 11, 11)
CONFIG = {
    "config": {
        "protocol_version": "v1.0.0",
        "scanner_name": "govulncheck",
        "scan_level": "symbol",
        "scan_mode": "source",
    }
}
OSV_6859 = {
    "osv": {
        "id": "GO-2026-6859",
        "aliases": ["CVE-2026-97054"],
        "details": "HTTP/1 server crash due to MaxBytesReader header race in net/http",
    }
}


def frame(module, package=None, function=None, filename=None, line=1, column=2, version=None):
    """Build one trace frame, matching govulncheck's JSON field names."""
    built = {"module": module}
    if version:
        built["version"] = version
    if package:
        built["package"] = package
    if function:
        built["function"] = function
    if filename:
        built["position"] = {"filename": filename, "offset": 1, "line": line, "column": column}
    return built


# The vulnerable symbol is reachable from pkg/mlflowclient/artifacts.go:43:40.
STDLIB_SYMBOL = [
    frame("stdlib", "net/http", "RoundTrip", "src/net/http/clientconn.go", 254, 23, "v1.27.3"),
    frame(
        "github.com/eval-hub/eval-hub",
        "github.com/eval-hub/eval-hub/pkg/mlflowclient",
        "UploadArtifact",
        "pkg/mlflowclient/artifacts.go",
        43,
        40,
    ),
]
# Imported but no vulnerable symbol is called.
STDLIB_PACKAGE = [frame("stdlib", "net/http", version="v1.27.3")]
# Dependency required by the module but never imported.
MODULE_ONLY = [frame("golang.org/x/crypto", version="v0.58.0")]


def finding(osv_id, trace, fixed="v1.27.4"):
    return {"finding": {"osv": osv_id, "fixed_version": fixed, "trace": trace}}


class GateTestCase(unittest.TestCase):
    """Base class with helpers that write reports and waiver files to a temp dir."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.tmp = Path(self._tmp.name)

    def report(self, *messages):
        path = self.tmp / "govulncheck.json"
        path.write_text("".join(json.dumps(message, indent=2) for message in messages), "utf-8")
        return path

    def waiver_file(self, *blocks):
        path = self.tmp / "waivers.toml"
        if not blocks:
            path.write_text("", "utf-8")
        else:
            path.write_text(
                "".join(f"[[IgnoredVulns]]\n{block}\n\n" for block in blocks), "utf-8"
            )
        return path

    def run_gate(self, report, waivers, today=TODAY):
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = gate.check(report, waivers, today, 3)
        return code, out.getvalue()


class WaivedFindingTests(GateTestCase):
    def test_waived_symbol_finding_passes(self):
        report = self.report(CONFIG, OSV_6859, finding("GO-2026-6859", STDLIB_SYMBOL))
        waivers = self.waiver_file(
            'id = "GO-2026-6859"\nignoreUntil = 2026-12-11\nreason = "toolchain not fixed yet"'
        )
        code, out = self.run_gate(report, waivers)
        self.assertEqual(code, 0, out)
        self.assertIn("WAIVED   GO-2026-6859", out)
        self.assertIn("waiver expires 2026-12-11", out)
        self.assertIn("CVE-2026-97054", out)
        self.assertIn("pkg/mlflowclient/artifacts.go:43:40", out)
        self.assertIn("1 waived, 0 informational, 0 blocking", out)

    def test_expired_waiver_fails(self):
        report = self.report(CONFIG, finding("GO-2026-6859", STDLIB_SYMBOL))
        waivers = self.waiver_file(
            'id = "GO-2026-6859"\nignoreUntil = 2026-11-10\nreason = "expired yesterday"'
        )
        code, out = self.run_gate(report, waivers)
        self.assertEqual(code, 1, out)
        self.assertIn("EXPIRED   GO-2026-6859", out)
        self.assertIn("waiver expired 2026-11-10", out)

    def test_expired_waiver_for_module_level_finding_fails(self):
        report = self.report(CONFIG, finding("GO-2026-5932", MODULE_ONLY, fixed="v0.58.1"))
        waivers = self.waiver_file(
            'id = "GO-2026-5932"\nignoreUntil = 2026-11-10\nreason = "expired yesterday"'
        )
        code, out = self.run_gate(report, waivers)
        self.assertEqual(code, 1, out)
        self.assertIn("EXPIRED   GO-2026-5932", out)
        self.assertIn("module level only", out)
        self.assertIn("waiver expired 2026-11-10", out)
        self.assertIn("0 waived, 0 informational, 1 blocking", out)

    def test_multiple_call_stacks_are_grouped_by_id(self):
        report = self.report(
            CONFIG,
            finding("GO-2026-6859", STDLIB_SYMBOL),
            finding("GO-2026-6859", STDLIB_PACKAGE),
            finding("GO-2026-6859", MODULE_ONLY),
        )
        waivers = self.waiver_file(
            'id = "GO-2026-6859"\nignoreUntil = 2026-12-11\nreason = "toolchain not fixed yet"'
        )
        code, out = self.run_gate(report, waivers)
        self.assertEqual(code, 0, out)
        self.assertEqual(out.count("WAIVED"), 1)
        self.assertIn("1 waived, 0 informational, 0 blocking", out)


class BlockingFindingTests(GateTestCase):
    def test_unwaived_symbol_finding_fails(self):
        report = self.report(CONFIG, finding("GO-2026-6859", STDLIB_SYMBOL))
        code, out = self.run_gate(report, self.waiver_file())
        self.assertEqual(code, 1, out)
        self.assertIn("VULNERABLE GO-2026-6859", out)
        self.assertIn("https://pkg.go.dev/vuln/GO-2026-6859", out)
        self.assertIn("Add a dated waiver to osv-scanner.toml", out)

    def test_mixed_ids_fail_on_the_unwaived_one(self):
        report = self.report(
            CONFIG,
            finding("GO-2026-6859", STDLIB_SYMBOL),
            finding("GO-2026-7000", STDLIB_SYMBOL, fixed="v1.27.5"),
        )
        waivers = self.waiver_file(
            'id = "GO-2026-6859"\nignoreUntil = 2026-12-11\nreason = "toolchain not fixed yet"'
        )
        code, out = self.run_gate(report, waivers)
        self.assertEqual(code, 1, out)
        self.assertIn("WAIVED   GO-2026-6859", out)
        self.assertIn("VULNERABLE GO-2026-7000", out)
        self.assertIn("1 waived, 0 informational, 1 blocking", out)


class InformationalFindingTests(GateTestCase):
    def test_module_and_package_findings_do_not_fail(self):
        report = self.report(
            CONFIG,
            finding("GO-2026-5932", MODULE_ONLY, fixed="v0.58.1"),
            finding("GO-2026-6859", STDLIB_PACKAGE),
        )
        code, out = self.run_gate(report, self.waiver_file())
        self.assertEqual(code, 0, out)
        self.assertIn("INFO     GO-2026-5932", out)
        self.assertIn("module level only", out)
        self.assertIn("INFO     GO-2026-6859", out)
        self.assertIn("package level only", out)
        self.assertIn("0 waived, 2 informational, 0 blocking", out)

    def test_waiver_for_module_level_finding_is_not_stale(self):
        report = self.report(CONFIG, finding("GO-2026-5932", MODULE_ONLY, fixed="v0.58.1"))
        waivers = self.waiver_file(
            'id = "GO-2026-5932"\nignoreUntil = 2026-12-31\nreason = "We do not use OpenPGP"'
        )
        code, out = self.run_gate(report, waivers)
        self.assertEqual(code, 0, out)
        self.assertIn("INFO     GO-2026-5932", out)
        self.assertNotIn("STALE", out)

    def test_stale_waiver_is_reported_without_failing(self):
        report = self.report(CONFIG, finding("GO-2026-5932", MODULE_ONLY))
        waivers = self.waiver_file(
            'id = "GO-2026-6859"\nignoreUntil = 2026-12-11\nreason = "toolchain not fixed yet"'
        )
        code, out = self.run_gate(report, waivers)
        self.assertEqual(code, 0, out)
        self.assertIn("STALE    GO-2026-6859", out)


class MalformedInputTests(GateTestCase):
    def test_report_without_config_is_rejected(self):
        report = self.report(finding("GO-2026-6859", STDLIB_SYMBOL))
        with self.assertRaises(gate.GateError):
            self.run_gate(report, self.waiver_file())

    def test_empty_report_is_rejected(self):
        report = self.report()
        with self.assertRaises(gate.GateError):
            self.run_gate(report, self.waiver_file())

    def test_truncated_report_is_rejected(self):
        path = self.tmp / "govulncheck.json"
        path.write_text('{"config": {"scan_level": "symbol"', "utf-8")
        with self.assertRaises(gate.GateError):
            self.run_gate(path, self.waiver_file())

    def test_report_without_finding_trace_is_rejected(self):
        report = self.report(CONFIG, {"finding": {"osv": "GO-2026-6859"}})
        with self.assertRaises(gate.GateError):
            self.run_gate(report, self.waiver_file())

    def test_report_with_empty_finding_trace_is_rejected(self):
        report = self.report(CONFIG, finding("GO-2026-6859", []))
        with self.assertRaises(gate.GateError):
            self.run_gate(report, self.waiver_file())

    def test_module_level_scan_report_is_rejected(self):
        config = {"config": dict(CONFIG["config"], scan_level=gate.MODULE)}
        report = self.report(config, finding("GO-2026-5932", MODULE_ONLY))
        with self.assertRaises(gate.GateError) as raised:
            self.run_gate(report, self.waiver_file())
        self.assertIn("scan_level='module'", str(raised.exception))

    def test_package_level_scan_report_is_rejected(self):
        config = {"config": dict(CONFIG["config"], scan_level=gate.PACKAGE)}
        report = self.report(config, finding("GO-2026-6859", STDLIB_PACKAGE))
        with self.assertRaises(gate.GateError) as raised:
            self.run_gate(report, self.waiver_file())
        self.assertIn("scan_level='package'", str(raised.exception))

    def test_report_without_scan_level_is_rejected(self):
        report = self.report({"config": {"scanner_name": "govulncheck"}})
        with self.assertRaises(gate.GateError) as raised:
            self.run_gate(report, self.waiver_file())
        self.assertIn("scan_level=None", str(raised.exception))

    def test_waiver_without_ignore_until_is_rejected(self):
        report = self.report(CONFIG, finding("GO-2026-6859", STDLIB_SYMBOL))
        waivers = self.waiver_file('id = "GO-2026-6859"\nreason = "no expiry"')
        with self.assertRaises(gate.GateError):
            self.run_gate(report, waivers)

    def test_waiver_without_reason_is_rejected(self):
        report = self.report(CONFIG, finding("GO-2026-6859", STDLIB_SYMBOL))
        waivers = self.waiver_file('id = "GO-2026-6859"\nignoreUntil = 2026-12-11')
        with self.assertRaises(gate.GateError):
            self.run_gate(report, waivers)


class CommandLineTests(GateTestCase):
    def test_cli_gates_a_waived_report(self):
        report = self.report(CONFIG, OSV_6859, finding("GO-2026-6859", STDLIB_SYMBOL))
        waivers = self.waiver_file(
            'id = "GO-2026-6859"\nignoreUntil = 2026-12-11\nreason = "toolchain not fixed yet"'
        )
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = gate.main(
                [
                    "--report",
                    str(report),
                    "--waivers",
                    str(waivers),
                    "--today",
                    TODAY.isoformat(),
                    "--max-examples",
                    "1",
                ]
            )
        self.assertEqual(code, 0, out.getvalue())
        self.assertIn("WAIVED   GO-2026-6859", out.getvalue())

    def test_cli_reports_a_gate_error(self):
        out = io.StringIO()
        err = io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = gate.main(["--report", str(self.tmp / "missing.json")])
        self.assertEqual(code, 1)
        self.assertIn("error: cannot read the govulncheck report", err.getvalue())


if __name__ == "__main__":
    unittest.main()
