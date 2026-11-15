# Contributing

Blackflame is a small, opinionated project. Keep changes focused and include tests.

## Development

Requirements:

- Go 1.27 or newer.
- No Node.js is required; the frontend is plain HTML/CSS/ES modules.

```sh
make test
make race
make vet
make build
```

Run the dashboard locally:

```sh
go run ./cmd/blackflame serve --mode ui --data ./data
```

The first run asks you to set an edit password. To set one ahead of time:

```sh
BLACKFLAME_PASSWORD='change-me' go run ./cmd/blackflame serve --mode ui --data ./data
```

## Pull requests

- Keep the change small and explain the user-facing behavior.
- Add or update tests for config, storage, discovery, auth, and API changes.
- Run `gofmt`, `go vet`, and `go test ./...` before opening a pull request.
- Update the relevant document under `docs/`, `PRODUCT.md`, or `ARCHITECTURE.md`
  when behavior or configuration changes.
- Sign off your commits with the Developer Certificate of Origin (`git commit -s`).

## Scope

0.1 focuses on the startpage, both configuration modes, discovery, status, and
Flame migration. Widgets, OIDC, quick actions, PWA, and NAS integration are
tracked in `ROADMAP.md` and should not be mixed into core changes without a
design discussion.

## Security

Please do not open a public issue for a security vulnerability. See
[SECURITY.md](SECURITY.md).
