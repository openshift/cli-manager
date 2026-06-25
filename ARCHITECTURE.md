# Architecture

## Overview

CLI Manager is an OpenShift operand (managed by [cli-manager-operator](https://github.com/openshift/cli-manager-operator)) that distributes CLI tools and kubectl plugins to cluster users via [krew](https://krew.sigs.k8s.io/). It solves the problem that standard krew indexes require internet connectivity by distributing plugins through container images and registries, enabling fully disconnected/air-gapped environments.

The operand's primary responsibilities:
- Watch `Plugin` CRs and reconcile plugin lifecycle
- Pull container images, extract plugin binaries
- Generate krew-compatible manifests with download URIs and SHA256 hashes
- Serve plugins via REST API and Git HTTP protocol
- Commit plugin artifacts to a local git repository serving as the krew index

## Data Flow

```text
  Plugin CR (config.openshift.io/v1alpha1)
  (name: any, cluster-wide scope)
              │
              ▼
  ┌──────────────────────────────────────────────────┐
  │         Plugin Controller (pkg/controller)        │
  │  (watches CR, pulls images, extracts files,       │
  │   generates krew manifest, commits to git repo)   │
  └──────────────────┬───────────────────────────────┘
                     │
      ┌──────────────┼──────────────────┐
      ▼              ▼                  ▼
  Git Repo      Image Pull         HTTP Server
  (krew index)  (go-containerregistry)  (REST + Git HTTP)
      │              │                  │
      └──────────────┴──────────────────┘
                     │
                     ▼
              Route (TLS edge)
              /cli-manager, /v1/plugins/download/
                     │
                     ▼
              oc krew (client)
```

Users run `oc krew index add ocp https://<route>/cli-manager`, then install plugins via `oc krew install ocp/<plugin>`.

## Operand Startup

Entry point: `cmd/cli-manager/main.go` → `pkg/cmd/cli-manager/cmd.go`.

Startup sequence:
1. Create clients (Kubernetes, dynamic, Route)
2. Initialize git repository at `GIT_REPO_PATH` (default: `/tmp/cli-manager-repo`)
3. Start controller (watches `Plugin` CRs with informer)
4. Start HTTP server on port 8080 (REST API + Git HTTP backend)
5. Block until context cancellation

## Custom Resource

The `Plugin` CRD (`config.openshift.io/v1alpha1`) defines plugin metadata and platform-specific binaries:

- **Spec fields**: `shortDescription`, `description`, `caveats`, `homepage`, `version`, `platforms[]`
  - Each platform: `platform` (os/arch regex `^(linux|darwin|windows)/(arm64|amd64|ppc64le|s390x)$`), `image`, `imagePullSecret`, `files[]`, `caBundle`, `proxyURL`, `bin`
  - Each file: `from` (absolute path in container), `to` (relative install path)
- **Status fields**: `conditions[]` (Available, Progressing, Degraded)

The CRD is cluster-scoped. Multiple Plugin CRs can exist (one per plugin). Platform format example: `linux/amd64`.

## Plugin Controller

`pkg/controller/controller.go` is the single controller. On each sync it:

1. Fetches the `Plugin` CR by name from the dynamic client
2. If NotFound (deletion): removes plugin files from git repo and returns
3. Deletes existing plugin artifacts (clean-slate approach)
4. Validates spec (platform regex, absolute file paths, no wildcards/symlinks)
5. For each platform:
   - Pulls container image (with ImagePullSecret auth, CA bundle, proxy support via `go-containerregistry`)
   - Extracts filesystem layers
   - Copies specified files to temp directory
   - Creates tar.gz archive and calculates SHA256 hash
6. Generates krew v1alpha2 manifest YAML with download URIs pointing to `/v1/plugins/download/?name=<plugin>&platform=<os/arch>`
7. Commits manifest and archives to git repository (`pkg/git/`)
8. Updates Plugin CR status conditions (Available, Progressing, Degraded)

The controller uses library-go's factory pattern with a rate-limited work queue. Events (add/update/delete) on Plugin CRs enqueue a sync by plugin name.

## HTTP Endpoints

The operand serves two endpoints (`pkg/cmd/cli-manager/`):

| Endpoint | Purpose |
|----------|---------|
| `GET /v1/plugins/download/?name=<plugin>&platform=<os/arch>` | Downloads plugin tar.gz archive (Content-Type: application/gzip) |
| `GET /cli-manager/` | Git HTTP backend serving the krew index (uses `net/http/cgi` wrapping `git http-backend`) |

The Route exposes these at `https://<route-host>/cli-manager` and `https://<route-host>/v1/plugins/download/`.

## Git Repository

`pkg/git/` manages a local git repo (default path: `/tmp/cli-manager-repo`) that stores krew manifests (`<plugin>.yaml`) and plugin archives (`<plugin>-<version>-<platform>.tar.gz`). The repo is initialized on startup and committed after each plugin update/deletion. The Git HTTP backend serves this repo at `/cli-manager` for krew clients.

**Why Git?** Krew natively supports Git-based indexes, provides version history, and works in disconnected environments.

## Build System

Uses `build-machinery-go`. Key targets:

| Target | Description |
|--------|-------------|
| `make build` | Build cli-manager binary (~5-10s) |
| `make test-unit` | Run unit tests in `pkg/...`, `cmd/...` (~10-30s) |
| `make test-e2e` | Run E2E tests (requires cluster, krew, ~3h) |
| `make verify` | Lint, format, verify generated code (~30-60s) |
| `make image-cli-manager` | Build container image |

**Build tags:** `strictfipsruntime`. **Base image:** `ubi9/ubi-minimal`. **Go version:** see `go.mod`.

## Testing

**Unit tests** (`make test-unit`): Co-located `*_test.go` files in `pkg/...`, `cmd/...`.

**E2E tests** (`make test-e2e`): `test/e2e/` deploys the operand to a real cluster, creates a Plugin CR, installs via krew, verifies execution, then cleans up. Requires OpenShift cluster, `KUBECONFIG`, and installed krew. Timeout: ~3h.

## Namespace

Everything runs in `openshift-cli-manager-operator` (constant `OperatorNamespace`). The operand Deployment, Service, Route, and RBAC all live here.

## Configuration

**Environment Variables:**

| Variable | Default | Purpose |
|----------|---------|---------|
| `WATCH_NAMESPACE` | (all) | Restrict Plugin CR watching to a single namespace |
| `GIT_REPO_PATH` | `/tmp/cli-manager-repo` | Local git repository path |

## Client Usage

Add the krew index:
```bash
ROUTE=$(oc get route/openshift-cli-manager -n openshift-cli-manager-operator -o=jsonpath='{.spec.host}')
oc krew index add ocp https://$ROUTE/cli-manager
```

Install plugins: `oc krew install ocp/<plugin>`, `oc krew search`, `oc krew update`, `oc krew remove <plugin>`.

**Self-Signed Certificates:** OpenShift Routes use self-signed certs. Clients must trust the cluster CA (see README.md for Fedora/macOS instructions).

## Directory Structure

| Directory / File | Purpose |
|-----------------|---------|
| `api/v1alpha1/` | Plugin CRD types (`plugin_types.go`, `groupversion_info.go`) |
| `cmd/cli-manager/` | Main operand entry point |
| `cmd/cli-manager-testing/` | Testing variant with insecure HTTP support |
| `pkg/cmd/cli-manager/` | Cobra command factory, HTTP server |
| `pkg/controller/` | Plugin controller reconciliation loop |
| `pkg/git/` | Git repository management (stores krew manifests, plugin archives) |
| `pkg/image/` | Container image ops (pull, extract files) |
| `pkg/krew/v1alpha2/` | Krew manifest types, conversion from Plugin CR |
| `test/e2e/` | End-to-end test suite |
| `vendor/` | Vendored dependencies (don't modify directly) |

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Container images for distribution | Leverages registry infrastructure in disconnected envs; no special packaging |
| Git backend for krew index | Krew native support; version history; atomic commits |
| Delete-then-recreate | Ensures clean state; avoids complex merge logic |
| Platform regex validation | Security: prevents arbitrary platform strings |
| Absolute file paths only | Security: no relative paths, wildcards, or symlinks |
| SHA256 hashes | Krew requirement; download integrity verification |

## Testing Variant

`cmd/cli-manager-testing/` builds an operand with `--serve-artifacts-in-http` (hidden flag). When enabled, the operand serves artifacts over HTTP for E2E tests in CI without TLS certificate setup.
