# ADR 0002: Portal login with a stateless ID-token session

**Status:** accepted (v0.2)

## Context
v0.1 relied on an OIDC-aware reverse proxy to inject `Authorization: Bearer <id_token>`. Operators asked
for sign-in without an extra component. The deployment is air-gapped and may run several replicas.

## Decision
- The API implements the OIDC authorization-code flow with PKCE (S256) and a nonce
  (`internal/auth/login.go`). State, nonce and verifier live in a 10-minute HttpOnly cookie scoped to `/auth/`.
- The session cookie (`kubestack_session`, HttpOnly, Secure when the redirect URL is https, SameSite=Lax)
  holds the **ID token itself**. Every request re-verifies its signature, audience and expiry, so there is no
  server-side session store and any replica can serve any request.
- CSRF: cookie-authenticated non-GET requests must carry `X-KubeStack-Request`. Browsers cannot add a custom
  header cross-site without a CORS preflight, and the API grants none. Bearer-token clients are unaffected.
- Bearer tokens remain supported; the login endpoints are mounted only when `KUBESTACK_OIDC_REDIRECT_URL` is set.

## Consequences
- Sessions last as long as the IdP's ID token (often 1 h); then the user signs in again. No refresh tokens
  are stored (nothing secret persists server-side, R15).
- Sign-out clears the KubeStack cookie only; it does not end the IdP session.
- A stolen cookie is usable until token expiry, same as a stolen bearer token.
