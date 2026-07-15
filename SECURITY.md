# Security Policy

## Supported Versions

Security fixes are applied on the `main` branch and included in subsequent releases.
Older release branches may receive backports when maintainers determine they remain supported.

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues, discussions, or pull requests.**

Use [GitHub private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability) for this repository:

1. Open the repository **Security** tab.
2. Click **Report a vulnerability**.
3. Include affected versions, impact, and steps to reproduce where possible.

Maintainers will acknowledge the report, investigate, and coordinate remediation and disclosure.

## Automated Security Checks

This repository runs automated security checks in CI, including:

- Gosec static analysis for Go code
- OpenSSF Scorecard for repository security posture

Results may appear under the repository **Security** tab when SARIF upload is enabled.
