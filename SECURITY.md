# Security Policy

## Supported Versions

| Version | Supported | Notes |
|---------|-----------|-------|
| v0.2.x  | ✅        | Current stable release. Security patches backported on request. |
| v0.1.x  | ❌        | End of life. |

## Reporting a Vulnerability

KubeStack is self-hosted software you run on your own infrastructure. There is no hosted service and no vendor security team.

If you find a security issue:

1. **Do not open a public GitHub issue.** Public issues expose the vulnerability before a fix is available.
2. Email the repository owner directly (see the GitHub profile for contact) or open a draft security advisory in the repository.
3. Include: affected version, steps to reproduce, impact, and any suggested mitigation.
4. Expect a response within 5 business days.

## Security by Design

| Area | Control | Evidence |
|------|---------|----------|
| Secrets | Never logged, persisted in plaintext, or returned in API responses | CLAUDE.md R15; `internal/audit/audit.go` records omit secrets |
| Authentication | OIDC (ID token as session cookie) or bearer token; CSRF header for cookie mutations | `internal/auth/auth.go`, `internal/auth/login.go` |
| Authorization | Admin group gate; capability flags per cluster tier | `domain.Cluster.Allowed()`; CLAUDE.md R6 |
| Idempotency | Destructive calls require `Idempotency-Key`; replays return stored result | `internal/store/store.go`; CLAUDE.md R17 |
| Audit trail | "Attempt" record written before any engine call; op refused if it fails | `internal/audit/audit.go` `Run()`; `internal/server/server.go` |
| Supply chain | Allowed licenses only; SBOM + cosign signature on every release | `hack/license-check.sh`; `.github/workflows/release.yml` |
| Runtime | No public-internet calls at runtime; air-gapped by default | CLAUDE.md R13–R14 |
| Container | Distroless, non-root, read-only root FS, least-privilege RBAC | `Dockerfile`; `deploy/helm/kubestack/templates/` |

## Disclosure Policy

Fixes are released as a new patch tag (`v0.2.x+1`). The security advisory is published with the release notes once a fix is available. There is no CVE assignment process; assigners may request a CVE through GitHub's private vulnerability reporting.
