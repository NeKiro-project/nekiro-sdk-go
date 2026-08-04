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

## Provenance

The package history was exported from
`NeKiro-project/NeKiro@aad73c450435a9b6c76c26cc6c525fa811b0e7ad`.
The original `sdks/` tree is
`b99c20c335322f623f850c14f20604d23c9d0079`, and the history-preserving export
commit is `1fe1f62ca17dd821a47c1d000734e2babeddee19`. The source repository retains
the annotated tag `pre-repository-split-2026-08-04` for original commit and
signature provenance.

Licensed under Apache-2.0. See `LICENSE`.
