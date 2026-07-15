# Security Policy

## Reporting a Vulnerability

The Eval Hub team takes security issues seriously. We appreciate your efforts to responsibly disclose your findings and will make every effort to acknowledge your contributions.

**Do not report security vulnerabilities through public GitHub issues, discussions, or pull requests.**

### Red Hat Product Security

To report a security vulnerability, please use the Red Hat Product Security contact:

- **Email**: [secalert@redhat.com](mailto:secalert@redhat.com)
- **PGP Key**: Available at https://access.redhat.com/security/team/key/

Red Hat Product Security will coordinate the investigation and disclosure process.

For more information on Red Hat's vulnerability response process, see:
https://access.redhat.com/security/vulnerability-policy/

### What to Include

When reporting a vulnerability, please include:

- A description of the vulnerability
- Steps to reproduce the issue
- Potential impact of the vulnerability
- Any suggested remediation (if available)

### Response Timeline

- **Acknowledgment**: We aim to acknowledge receipt of your report within 3 business days.
- **Assessment**: We will provide an initial assessment within 10 business days.
- **Resolution**: We will work to resolve confirmed vulnerabilities promptly and coordinate disclosure with you.

## Supported Versions

Security updates are applied to the latest release. We recommend always running the most recent version of Eval Hub.

## Security Best Practices

When deploying Eval Hub:

- Run behind [kube-rbac-proxy](https://github.com/brancz/kube-rbac-proxy) or equivalent authentication proxy in production
- Use TLS for all network communication
- Follow the principle of least privilege for service account permissions
- Keep dependencies up to date
