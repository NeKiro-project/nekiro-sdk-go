# NeKiro Go SDK

This repository is the canonical source for NeKiro's public Go SDKs:

- `agent`: runtime-neutral managed Agent-to-Agent invocation through the NeKiro A2A Router.
- `agent/routerauth`: verification and middleware for Router-issued invocation credentials.
- `client`: application invocation of Agents already installed in one Workspace.

The SDK depends only on the public `github.com/NeKiro-project/NeKiro/contracts`
package. It does not import core service internals, discover endpoints, retry,
select alternate components, or provide an Agent Runtime.

```bash
go get github.com/NeKiro-project/nekiro-sdk-go@<reviewed-release>
```

Import packages with:

```go
import (
    agentsdk "github.com/NeKiro-project/nekiro-sdk-go/agent"
    "github.com/NeKiro-project/nekiro-sdk-go/agent/routerauth"
    clientsdk "github.com/NeKiro-project/nekiro-sdk-go/client"
)
```

Compatibility is explicit in `go.mod`. Consumers must update the SDK and core
contract versions deliberately; local `replace` directives and floating source
references are unsupported.

## Development and verification

Run the complete SDK-owned checks from the repository root:

```text
go build ./...
go test -count=1 ./...
go test -race ./...
go vet ./...
go mod tidy
go mod verify
git diff --check
```

Verification succeeds when every package builds, all Agent/auth/client tests
pass, the race detector and vet report no findings, and `go mod tidy` leaves
the module files unchanged. Contract-boundary tests must also confirm that no
SDK package imports a Core service implementation package.

The `Core compatibility` workflow can test the canonical SDK source against an
explicit full Core commit SHA. It temporarily resolves that exact public Core
module revision in the CI checkout, runs the public-contract suite, and never
commits the changed module files or adds a local `replace` directive. Core uses
this reusable workflow after every merge to `main`.

## RepoWiki

The [NeKiro Go SDK RepoWiki](https://nekiro-project.github.io/nekiro-sdk-go/)
publishes the SDK documentation in English and Chinese with MkDocs Material.
The source README files remain canonical.

## Pull requests

Pull requests must identify affected public packages and types, the exact Core
contract revision used for verification, compatibility impact, commands run,
and observable success signals. A public API break requires an explicit
versioning and migration decision.

## Provenance

The package history was exported from
`NeKiro-project/NeKiro@aad73c450435a9b6c76c26cc6c525fa811b0e7ad`.
The original `sdks/` tree is
`b99c20c335322f623f850c14f20604d23c9d0079`, and the history-preserving export
commit is `1fe1f62ca17dd821a47c1d000734e2babeddee19`. The source repository retains
the annotated tag `pre-repository-split-2026-08-04` for original commit and
signature provenance.

Licensed under Apache-2.0. See `LICENSE`.
