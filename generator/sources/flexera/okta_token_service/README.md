# Okta Token Service

OpenAPI spec for the Flexera Okta Token Service — the service that handles
requests to `login.flexera.com` (and the `.au` / `.eu` variants) to generate
JWT tokens used to
[authenticate with the Flexera API](https://developer.flexera.com/docs/page/authentication).

This spec is hand-maintained in this repository.  It documents the endpoints
that the unified Flexera One SDK helpers depend on (currently token generation
and session management).  New endpoints should be added only when an SDK helper
requires them.
