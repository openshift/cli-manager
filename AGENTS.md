# AI Agent Guide for CLI Manager

This file provides guidance for AI agents working with the OpenShift CLI Manager repository.

## Overview

**What is CLI Manager?**
An OpenShift operand (managed by the [cli-manager-operator](https://github.com/openshift/cli-manager-operator)) that distributes CLI tools and kubectl plugins to cluster users via [krew](https://krew.sigs.k8s.io/). It solves the problem that standard krew indexes require internet connectivity by distributing plugins through container images and registries, enabling fully disconnected/air-gapped environments.

 The CLI Manager is installed and lifecycle-managed by the CLI Manager Operator, which is in turn installed by the Operator Lifecycle Manager (OLM). The operator reconciles the cli-manager CR; this repo contains the operand binary that actually serves plugins.

## Build and Test

```bash
make build        # Build all binaries 
make test-unit    # Unit tests 
make verify       # Formatting, vetting, golang version checks
make test-e2e     # E2E tests
```

Go version: see `go.mod`.

## Project Structure

| Directory / File | Purpose |
|-----------------|---------|
| `api/v1alpha1/` | `Plugin` CRD type definitions (`plugin_types.go`, `groupversion_info.go`) |
| `cmd/cli-manager/` | Main operand binary entry point |
| `cmd/cli-manager-testing/` | Testing variant that enables insecure HTTP artifact serving |
| `pkg/cmd/cli-manager/` | Cobra command factory, HTTP server implementation |
| `pkg/controller/controller.go` | Main reconciliation loop — watches `Plugin` CRs and manages plugin lifecycle |
| `pkg/git/git.go` | Git repository management — stores and serves krew manifests and plugin archives |
| `pkg/image/extract.go` | Container image operations — pulls images, extracts plugin binaries from layers |
| `pkg/krew/v1alpha2/` | Krew v1alpha2 manifest types and conversion from `Plugin` CR |
| `pkg/version/version.go` | Build version info |
| `test/e2e/` | E2E test suite — deploys operand, creates `Plugin` CR, validates krew install |
| `hack/` | Build and development scripts |
| `vendor/` | Vendored dependencies — **do not modify directly** |
| `.tekton/` | Tekton CI/CD pipeline definitions |
| `Dockerfile` | Operand container image build |
| `Dockerfile.ci` | CI-specific container build |
| `Makefile` | Build targets |
| `go.mod` | Go module dependencies |
| `README.md` | User-facing documentation |

## Controller Pattern

The operand has a single controller wired in `pkg/controller/controller.go` via the library-go factory pattern:

**`Plugin` Controller** — the main reconciliation loop. Watches `Plugin` CRs and reconciles the full plugin lifecycle on every add/update/delete event via a 9-step pipeline:

```
validate → pull images → extract files → generate krew manifests → commit to git
```
**Key Concepts:**
- **Informer:** Watches for `Plugin` CR changes
- **QueueKeyFunc:** Extracts plugin name as reconciliation key
- **Sync:** Main reconciliation function called on every event
- **EventRecorder:** Records Kubernetes events for debugging

## Key Conventions

- **Namespace:** The operand runs in `openshift-cli-manager-operator`. Constants live in `pkg/operator/operatorclient/interfaces.go` (in the operator repo).
- **Plugin CRD:** Platform format is `os/arch` (e.g., `linux/amd64`, `darwin/arm64`). Regex: `^(linux|darwin|windows)/(arm64|amd64|ppc64le|s390x)$`.
- **HTTP endpoints:**
  - `GET /v1/plugins/download/` — serves plugin archives
  - `GET /cli-manager/` — Git HTTP backend for krew index
- **Logging:** `k8s.io/klog/v2` with verbosity levels (`--v=4` informational, `--v=6` debug traces).
- **Error handling:** wrap with `fmt.Errorf("context: %w", err)`; return retriable errors, return `nil` for non-retriable (validation) errors.
- **CRD changes:** Modify `api/v1alpha1/`, then run `make update-codegen`.

## Critical Rules

### DO NOT
1. **Don't modify CRD definitions** in `api/v1alpha1/` without understanding backward compatibility implications
2. **Don't modify `vendor/`** — always use `go mod tidy && go mod vendor`
3. **Don't skip `make verify`** before considering work complete
4. **Don't log secrets** — ImagePullSecrets, CA bundles, or auth tokens must never appear in logs
5. **Don't use relative paths** in file extraction logic (security risk)
6. **Don't allow wildcard or symlink file paths** in Plugin CR validation
7. **Don't modify OWNERS files** without explicit direction from maintainers

### DO
1. **Run `make verify`** before submitting any changes
2. **Run `make test-unit`** to ensure tests pass
3. **Use structured logging** via klog with appropriate verbosity levels
4. **Follow Kubernetes API conventions** for CRD status conditions
5. **Handle errors gracefully** and return meaningful error messages
6. **Use the controller factory pattern** from library-go
7. **Validate all Plugin CR inputs** (platform format, file paths, version strings)
8. **Document architectural decisions** in ARCHITECTURE.md

## Non-Obvious Internals

- **`controllercmd` intermediary:** The entry point chain (`cmd/` → `pkg/cmd/cli-manager/`) passes through library-go's `controllercmd.ControllerCommandConfig`, which handles signal handling, health checks, and serving info. Leader election is managed by the operator, not the operand.
- **Git-based plugin distribution:** Plugins are not served directly from OCI images at request time. The reconciler extracts binaries at reconciliation time, stores them in a local git repo, and serves the repo via Git's HTTP smart protocol — this is what krew fetches.
- **Adding a new HTTP endpoint:** Requires changes to the server setup in `pkg/cmd/cli-manager/` and corresponding handler logic. The git HTTP backend is handled via `net/http/cgi` wrapping `git http-backend`.
- **CRD generation:** `api/v1alpha1/zz_generated.deepcopy.go` is generated — edit `plugin_types.go`, then run `make update-codegen`, never edit the generated file directly.

### Updating Dependencies

1. Update `go.mod`: `go get <module>@<version> && go mod tidy`
2. Vendor: `go mod vendor`
3. Verify: `make verify && make test-unit`


## Testing

- **Unit tests:** co-located `*_test.go` files, table-driven, run with `make test` or `go test ./pkg/... ./cmd/...`
- **E2E tests:** `test/e2e/` — deploys the operand to a real cluster, creates a `Plugin` CR, installs it via krew, and verifies execution. Run with `make test-e2e` (requires cluster, ~3h timeout).

## Additional Resources

- [ARCHITECTURE.md](ARCHITECTURE.md) — Complete system design, components, and technical details
- [README.md](README.md) — User-facing documentation and getting started guide
- [cli-manager-operator](https://github.com/openshift/cli-manager-operator) — Operator that manages this operand
- [Krew Plugin Manifest Spec](https://krew.sigs.k8s.io/docs/developer-guide/plugin-manifest/)
- [OpenShift library-go](https://github.com/openshift/library-go) — Controller factory patterns
- [go-containerregistry](https://github.com/google/go-containerregistry) — Image operations
