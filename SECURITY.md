# Security Policy

## Supported Versions

| Version | Supported |
|---|---|
| Latest release | Yes |
| Previous minor | Best-effort |
| Older | No |

We recommend always running the latest release.

## Reporting a Vulnerability

**Do not open a public issue for security vulnerabilities.**

Instead, please report security issues by emailing the maintainers directly or using [GitHub's private vulnerability reporting](https://github.com/bearbinary/omni-infra-provider-truenas/security/advisories/new).

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

We will acknowledge receipt within 48 hours and aim to provide a fix or mitigation within 7 days for critical issues.

## Scope

This policy covers the `omni-infra-provider-truenas` codebase and its Docker image. Issues in upstream dependencies (TrueNAS, Omni, Talos) should be reported to their respective projects.

## Security Considerations

- **API keys**: The provider handles Omni service account keys and TrueNAS API keys. These are passed via environment variables and never logged.
- **Transport security**: WebSocket connections use TLS by default. Unix socket transport relies on filesystem permissions.
- **Container security**: The Docker image runs as a non-root user (UID 65534) with a read-only filesystem and all capabilities dropped.
- **No secrets in images**: Credentials are injected at runtime, never baked into the container image.
