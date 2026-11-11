#!/usr/bin/env python3
"""Fail ``make govulncheck`` on reachable vulnerabilities that are not waived.

govulncheck cannot silence a finding (https://go.dev/issue/61211) and always
exits 0 when a machine-readable format is requested, so the Makefile pipes its
streaming JSON report through this gate. The gate reads the ``[[IgnoredVulns]]``
waivers recorded in ``osv-scanner.toml`` (each needs ``ignoreUntil`` and
``reason``), prints one line per vulnerability with example call sites, and
exits non-zero when a symbol-level finding is unwaived or its waiver expired.
An expired waiver fails the gate whatever the finding's level, so an entry
cannot be forgotten by recording it against a module- or package-level finding.

Findings are grouped by OSV ID: govulncheck emits one finding per call stack, so
a single advisory can appear dozens of times in one report. Package- and
module-level findings are printed but do not fail, which matches the exit status
of the text output (only reachable symbols make govulncheck exit 3). Reports that
govulncheck produced with ``-scan=module`` or ``-scan=package`` are rejected
instead: without symbol-level analysis every finding is informational, so such a
report could never fail the gate.

Run directly against a saved report:

    python3 scripts/check_govulncheck_waivers.py --report bin/govulncheck.json
"""

import argparse
import datetime
import json
import sys
import tomllib
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
DEFAULT_WAIVERS = REPO_ROOT / "osv-scanner.toml"
VULN_URL = "https://pkg.go.dev/vuln/{}"

# Finding levels, ordered from the least to the most severe. A vulnerability can
# be reported at several levels at once; the most severe one drives the gate.
MODULE = "module"
PACKAGE = "package"
SYMBOL = "symbol"
SEVERITY = {MODULE: 0, PACKAGE: 1, SYMBOL: 2}


class GateError(Exception):
    """A report or waiver file that cannot be interpreted."""


class Waiver:
    """One dated ``[[IgnoredVulns]]`` entry."""

    def __init__(self, vuln_id, ignore_until, reason):
        self.vuln_id = vuln_id
        self.ignore_until = ignore_until
        self.reason = reason

    def expired(self, today):
        return self.ignore_until < today


class Vulnerability:
    """All findings govulncheck reported for one OSV ID."""

    def __init__(self, vuln_id):
        self.vuln_id = vuln_id
        self.level = MODULE
        self.fixed_version = ""
        self.origin = ""
        self.package = ""
        self.aliases = []
        self.details = ""
        self.call_sites = []

    def add_finding(self, finding):
        level = finding_level(finding)
        if SEVERITY[level] > SEVERITY[self.level]:
            self.level = level
        self.fixed_version = finding.get("fixed_version") or self.fixed_version
        origin, package = origin_of(finding)
        self.origin = origin or self.origin
        self.package = package or self.package
        site = call_site(finding)
        if site and site not in self.call_sites:
            self.call_sites.append(site)


class Report:
    """The parts of a govulncheck JSON stream the gate needs."""

    def __init__(self, path):
        self.path = path
        self.config = None
        self.vulnerabilities = {}
        self.osvs = {}

    def add_finding(self, finding):
        vuln_id = finding.get("osv")
        if not vuln_id:
            raise GateError(f"{self.path}: finding without an OSV ID")
        self.vulnerabilities.setdefault(vuln_id, Vulnerability(vuln_id)).add_finding(finding)

    def add_osv(self, osv):
        vuln_id = osv.get("id")
        if vuln_id:
            self.osvs[vuln_id] = osv

    def require_symbol_scan_level(self):
        """Reject reports that govulncheck did not scan at symbol level.

        ``-scan=module`` and ``-scan=package`` report every vulnerable dependency
        without proving that the vulnerable symbol is reachable, and ``evaluate``
        treats those findings as informational, so such a report would let an
        unwaived vulnerability pass the gate.
        """
        if not isinstance(self.config, dict):
            raise GateError(f"{self.path}: govulncheck config message is not an object")
        scan_level = self.config.get("scan_level")
        if scan_level != SYMBOL:
            raise GateError(
                f"{self.path}: govulncheck ran with scan_level={scan_level!r}, but the "
                f"waiver gate requires {SYMBOL!r}. Run govulncheck without -scan "
                "(symbol is the default) and regenerate the report."
            )

    def apply_osvs(self):
        """Attach aliases and details, which govulncheck streams before findings."""
        for vuln_id, vulnerability in self.vulnerabilities.items():
            osv = self.osvs.get(vuln_id)
            if osv is None:
                continue
            vulnerability.aliases = osv.get("aliases") or []
            vulnerability.details = (osv.get("details") or "").strip()


def finding_level(finding):
    """Return ``symbol``, ``package`` or ``module`` for one finding.

    govulncheck always reports at least one trace frame, even for a module-level
    finding, so a finding without one is malformed. Reject it instead of calling
    it module level: module-level findings are informational and never fail the
    gate, so an uninterpretable finding must not be silently downgraded.
    """
    trace = finding.get("trace") or []
    if not trace:
        vuln_id = finding.get("osv") or "<unknown OSV ID>"
        raise GateError(f"finding {vuln_id} has no trace frames")
    if any(frame.get("function") for frame in trace):
        return SYMBOL
    if any(frame.get("package") for frame in trace):
        return PACKAGE
    return MODULE


def origin_of(finding):
    """Return (module@version, package) of the vulnerable symbol."""
    trace = finding.get("trace") or []
    if not trace:
        return "", ""
    frame = trace[0]
    origin = frame.get("module") or ""
    if origin and frame.get("version"):
        origin = f"{origin}@{frame['version']}"
    return origin, frame.get("package") or ""


def call_site(finding):
    """Return the outermost frame position, i.e. the call site in this module."""
    for frame in reversed(finding.get("trace") or []):
        position = frame.get("position") or {}
        filename = position.get("filename")
        if filename:
            line = position.get("line", "?")
            column = position.get("column", "?")
            return f"{filename}:{line}:{column}"
    return ""


def read_report(path):
    """Parse a govulncheck JSON stream (concatenated, possibly indented objects)."""
    try:
        text = Path(path).read_text(encoding="utf-8")
    except OSError as exc:
        raise GateError(f"cannot read the govulncheck report {path}: {exc}") from exc

    report = Report(path)
    decoder = json.JSONDecoder()
    index = 0
    while True:
        while index < len(text) and text[index].isspace():
            index += 1
        if index >= len(text):
            break
        try:
            message, index = decoder.raw_decode(text, index)
        except json.JSONDecodeError as exc:
            raise GateError(f"{path}: not a govulncheck JSON stream: {exc}") from exc
        if not isinstance(message, dict):
            raise GateError(f"{path}: expected a JSON object, got {type(message).__name__}")
        if "config" in message:
            report.config = message["config"]
        elif "finding" in message:
            report.add_finding(message["finding"])
        elif "osv" in message:
            report.add_osv(message["osv"])

    if report.config is None:
        raise GateError(
            f"{path}: no govulncheck config message, so the report is empty or incomplete"
        )
    report.require_symbol_scan_level()
    report.apply_osvs()
    return report


def load_waivers(path):
    """Return {OSV ID: Waiver} from the [[IgnoredVulns]] entries in ``path``."""
    try:
        data = tomllib.loads(Path(path).read_text(encoding="utf-8"))
    except OSError as exc:
        raise GateError(f"cannot read the waiver file {path}: {exc}") from exc
    except tomllib.TOMLDecodeError as exc:
        raise GateError(f"{path}: invalid TOML: {exc}") from exc

    waivers = {}
    for entry in data.get("IgnoredVulns", []):
        vuln_id = entry.get("id")
        if not vuln_id:
            raise GateError(f"{path}: [[IgnoredVulns]] entry without an id")
        ignore_until = entry.get("ignoreUntil")
        if not isinstance(ignore_until, datetime.date):
            raise GateError(
                f"{path}: waiver {vuln_id} needs an ignoreUntil TOML date "
                "(for example ignoreUntil = 2026-12-11)"
            )
        reason = (entry.get("reason") or "").strip()
        if not reason:
            raise GateError(f"{path}: waiver {vuln_id} needs a reason")
        waivers[vuln_id] = Waiver(vuln_id, ignore_until, reason)
    return waivers


def describe(vulnerability):
    """Return the trailing details of one vulnerability line."""
    parts = []
    if vulnerability.origin:
        origin = vulnerability.origin
        if vulnerability.package and vulnerability.package not in vulnerability.origin:
            origin = f"{origin} ({vulnerability.package})"
        parts.append(f"found in {origin}")
    if vulnerability.fixed_version:
        parts.append(f"fixed in {vulnerability.fixed_version}")
    if vulnerability.aliases:
        parts.append(", ".join(vulnerability.aliases))
    if vulnerability.level != SYMBOL:
        parts.append(f"{vulnerability.level} level only")
    return "; ".join(parts)


def print_call_sites(vulnerability, max_examples):
    """Print a bounded list of call sites for one vulnerability."""
    if not vulnerability.call_sites:
        return
    shown = vulnerability.call_sites[:max_examples]
    suffix = ""
    if len(vulnerability.call_sites) > len(shown):
        suffix = f" (+{len(vulnerability.call_sites) - len(shown)} more call stacks)"
    print(f"         reachable via {', '.join(shown)}{suffix}")


def evaluate(report, waivers, today, max_examples):
    """Print the gate summary and return the process exit code."""
    blocked = []
    waived = []
    informational = []
    reported = set(report.vulnerabilities)

    for vuln_id in sorted(report.vulnerabilities):
        vulnerability = report.vulnerabilities[vuln_id]
        waiver = waivers.get(vuln_id)
        # Expiry is checked before the level: a waiver that has passed its date
        # must fail the gate even when the finding is only module or package
        # level, otherwise recording a waiver would silence the deadline.
        if waiver is not None and waiver.expired(today):
            blocked.append(vulnerability)
            continue
        if vulnerability.level != SYMBOL:
            informational.append(vulnerability)
            continue
        if waiver is None:
            blocked.append(vulnerability)
            continue
        waived.append((vulnerability, waiver))

    for vulnerability, waiver in waived:
        print(
            f"WAIVED   {vulnerability.vuln_id}  {describe(vulnerability)}; "
            f"waiver expires {waiver.ignore_until.isoformat()}"
        )
        print(f"         reason: {waiver.reason}")
        print_call_sites(vulnerability, max_examples)

    for vulnerability in informational:
        print(f"INFO     {vulnerability.vuln_id}  {describe(vulnerability)}")

    for vulnerability in blocked:
        waiver = waivers.get(vulnerability.vuln_id)
        label = "EXPIRED  " if waiver is not None else "VULNERABLE"
        print(f"{label} {vulnerability.vuln_id}  {describe(vulnerability)}")
        if waiver is not None:
            print(
                f"         waiver expired {waiver.ignore_until.isoformat()}: "
                f"{waiver.reason}"
            )
        print(f"         more info: {VULN_URL.format(vulnerability.vuln_id)}")
        if vulnerability.details:
            print(f"         {vulnerability.details}")
        print_call_sites(vulnerability, max_examples)

    for vuln_id in sorted(set(waivers) - reported):
        waiver = waivers[vuln_id]
        print(
            f"STALE    {vuln_id}  waiver dated {waiver.ignore_until.isoformat()} but "
            "govulncheck reported nothing for this ID: remove the entry once the "
            "toolchain or module is fixed"
        )

    total = len(waived) + len(informational) + len(blocked)
    print(
        f"\ngovulncheck waiver gate: {len(waived)} waived, {len(informational)} "
        f"informational, {len(blocked)} blocking ({total} reported)"
    )
    if blocked:
        print("Add a dated waiver to osv-scanner.toml or fix the vulnerability.")
        return 1
    return 0


def check(report_path, waivers_path, today, max_examples):
    """Evaluate one report and return the process exit code."""
    report = read_report(report_path)
    waivers = load_waivers(waivers_path)
    return evaluate(report, waivers, today, max_examples)


def main(argv=None):
    parser = argparse.ArgumentParser(
        description=(
            "Fail on govulncheck findings that are not covered by a dated "
            "[[IgnoredVulns]] waiver in osv-scanner.toml."
        )
    )
    parser.add_argument(
        "--report", required=True, type=Path, help="govulncheck -format=json output"
    )
    parser.add_argument(
        "--waivers",
        type=Path,
        default=DEFAULT_WAIVERS,
        help=f"waiver file (default: {DEFAULT_WAIVERS})",
    )
    parser.add_argument(
        "--today",
        type=datetime.date.fromisoformat,
        default=None,
        help="evaluate waiver expiry as of this YYYY-MM-DD date (default: today)",
    )
    parser.add_argument(
        "--max-examples",
        type=int,
        default=3,
        help="call sites to print per vulnerability (default: 3)",
    )
    args = parser.parse_args(argv)

    today = args.today or datetime.date.today()
    try:
        return check(args.report, args.waivers, today, args.max_examples)
    except GateError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
