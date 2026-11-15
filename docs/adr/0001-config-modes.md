# ADR 0001: Two explicit configuration modes

- **Status:** Accepted
- **Date:** 2026-11-13
- **Deciders:** Blackflame maintainers
- **Related:** [Architecture](../../ARCHITECTURE.md), [Product](../../PRODUCT.md)

## Context

Blackflame serves two audiences with different operational preferences:

1. **GUI users** want to add apps, bookmarks, categories, themes, and settings in the browser. This is Flame's primary workflow.
2. **GitOps/Kubernetes users** want the dashboard defined in a YAML file stored in Git or a ConfigMap. They expect the running instance to reflect that file and do not want a UI mutating cluster state.

A common proposal is to keep both a SQLite database and a YAML file and synchronize them. That approach creates a second source of truth, ambiguous merge behavior, and hard-to-debug drift:

- Which side wins when both changed?
- Do UI edits write back to Git?
- What happens when a ConfigMap reload races with a UI edit?
- How are deletions represented?
- How do secrets stay out of Git?

The product also needs runtime state that is not content configuration: sessions, status snapshots, icon cache, and discovery results. Mixing runtime state into the content config makes exports noisy and reloads destructive.

## Decision

Blackflame supports **two explicit configuration modes**, selected at startup:

### UI mode

- Flag: `--mode ui`.
- Required: `--data` directory.
- SQLite is the authoritative source for boards, sections, items, overrides, and settings.
- The browser can mutate content.
- YAML is available for import and export only.
- Runtime state lives in the same SQLite database by default.

### GitOps mode

- Flag: `--mode gitops`.
- Required: `--config` YAML file.
- YAML is the authoritative source for content and settings.
- The browser is read-only; mutation endpoints return `409 Conflict`.
- YAML is validated and atomically swapped into an immutable in-memory snapshot.
- Invalid reloads keep the last good snapshot and surface an error.
- Runtime state can live in SQLite via `--data`, or in memory if `--data` is omitted.
- Discovery overrides live in YAML; discovery results remain runtime data.
- Secret-bearing widget config (later) uses `valueFrom: env|file` references, not plaintext. The 0.1 edit password comes from the environment or a file.

### Shared invariants

- Exactly one authoritative source per mode.
- No bidirectional synchronization between YAML and SQLite.
- The same internal content model and API shape are used in both modes.
- Runtime state is never exported as content.
- Flame import can target either mode: SQLite for UI mode, YAML for GitOps mode.

## Consequences

### Positive

- Clear semantics: users know where the truth lives.
- No merge conflicts or drift between two writable stores.
- GitOps users get a read-only, auditable configuration path.
- UI users keep Flame's editing experience.
- The same query/merge/visibility code serves both modes.
- Runtime state can be discarded without losing content.

### Negative

- Two storage implementations and two test paths.
- GitOps users cannot make ad-hoc edits in the UI; they must edit YAML.
- UI users must export/import to move to GitOps mode.
- YAML reload needs careful validation, atomicity, and error reporting.
- In-memory runtime mode loses sessions, status, and icons on restart.

### Neutral

- The frontend must display a "managed by config" state and disable editing controls in GitOps mode.
- The API must return a clear conflict error for mutations in GitOps mode.
- Documentation must explain the migration path between modes.

## Alternatives considered

### SQLite only

Simplest for Flame parity, but fails the GitOps/Kubernetes use case and forces a PVC for every deployment.

### YAML only

Excellent for GitOps, but loses Flame's core GUI editing workflow and makes multi-board editing painful.

### Bidirectional sync

Rejected. It creates two sources of truth, ambiguous conflict resolution, and likely data loss. The apparent convenience is not worth the operational complexity.

### Hybrid merge at read time

Rejected for 0.1. Merging independently edited YAML and SQLite content requires precedence rules, deletion tracking, and a UI that explains which fields are overridden. It can be revisited only with a strong product reason.

## Implementation notes

- Define a `ConfigStore` interface with `Snapshot()`, `Reload()`, `Watch()`, `ReadOnly()`, and `Close()`. UI-mode mutation is handled by the SQLite store and admin API; GitOps mode rejects mutations at the HTTP layer.
- Implement `SQLiteStore` and `YAMLStore`.
- Make the snapshot immutable and validated before it becomes visible.
- Use a single internal model for boards, sections, items, overrides, and settings.
- Run all visibility filtering and discovery merging against the snapshot.
- In GitOps mode, reject mutations at the HTTP layer, not only in the UI.
- Expose config status (loaded, last reload, error) through the API and UI.
- Use a periodic hash check so Kubernetes ConfigMap symlink swaps are detected without requiring filesystem notifications.
- Support `SIGHUP` for an explicit reload.
- Provide `blackflame validate --config` and `blackflame export`.
- Add tests for: valid reload, invalid reload, last-good preservation, mutation rejection, and runtime state separation.

## Follow-ups

- Decide whether GitOps mode should support multiple YAML documents or a single file. Default: single file for simplicity.
- Decide the exact schema versioning and deprecation policy before 1.0.
- Document migration from UI mode to GitOps mode and back.
