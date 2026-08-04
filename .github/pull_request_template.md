## Summary

<!-- Describe the SDK behavior changed and the owning public package. -->

## Compatibility and cross-repository impact

- Affected public packages/types:
- Core contract revision tested:
- Samples or Stack follow-up:

- [ ] No public API or wire behavior changed.
- [ ] Compatible additions preserve existing callers.
- [ ] Breaking changes include versioning and migration evidence.

## Verification

Commands run:

```text
go build ./...
go test -count=1 ./...
go test -race ./...
go vet ./...
go mod tidy
go mod verify
git diff --check
```

Observed success signals:

<!-- Include contract-boundary tests and the exact Core compatibility result. -->

## Security and failure semantics

- [ ] Credentials and invocation payloads are not logged or persisted.
- [ ] Missing, invalid, unauthorized, timeout, cancellation, and dependency failures remain distinct.
- [ ] No discovery, alternate endpoint, retry, compatibility shim, or local replacement was introduced without policy evidence.

Fallback delta: removed 0, retained 0, added 0, net 0

Added fallback evidence: none

## Checklist

- [ ] The SDK imports only public Core contracts.
- [ ] Public examples and README guidance remain accurate.
- [ ] Tests cover affected success and failure paths.
- [ ] Cross-repository dependencies use immutable reviewed revisions.
