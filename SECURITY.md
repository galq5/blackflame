# Security policy

Blackflame holds credentials for other homelab services and can be exposed to a
network. Please treat security reports privately.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting for the repository, or contact the
maintainer directly if that is not available. Include:

- affected version or commit;
- a description of the issue;
- reproduction steps;
- any suggested fix or mitigation.

Do not include real credentials, private keys, or personal data in a report.
Please allow time for a fix before public disclosure.

## Scope

In scope:

- authentication and session handling;
- CSRF and private-content leakage;
- SVG/icon sanitization;
- server-side request forgery through status checks or widgets;
- path traversal and unsafe file handling;
- Docker/Kubernetes discovery permission issues;
- container or deployment hardening.

Out of scope:

- issues that require an already-compromised administrator account;
- misconfigured reverse proxies or exposed Docker sockets;
- vulnerabilities in third-party services that Blackflame merely links to.

## Default posture

- No default password.
- Set `BLACKFLAME_PASSWORD` before exposing a fresh instance to an untrusted network; otherwise the first visitor can claim the setup screen.
- Private content is filtered server-side.
- Discovery is read-only.
- No arbitrary command execution.
- No telemetry.
- Container images run as non-root and support a read-only root filesystem.

## Proxies and rate limiting

Login attempts are rate limited per source address, and `X-Forwarded-For` is
ignored by default: the header is client-controlled and trusting it would let
an attacker bypass the limiter with arbitrary values. If Blackflame runs behind
a reverse proxy that terminates client connections, set
`BLACKFLAME_TRUST_FORWARDED_FOR=1` to key the limiter on the appended
(rightmost) forwarded entry instead of the proxy address. Only enable it when
every request reaches the app through that proxy.
