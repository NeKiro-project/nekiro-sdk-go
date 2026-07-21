# Agent SDK

`sdks/agent-sdk` is a small Go client for the managed Agent Router v1 nested
invocation boundary. It carries the trusted platform context supplied by the
managed transport, validates the untrusted target request, and performs one
HTTP call through the Router.

The SDK does not implement a model, tool, workflow, memory, retry, cache,
fallback route, or Agent Runtime. `NewClient` requires explicit response and
SSE event byte limits; there are no size defaults. Use `Invoke` for JSON and
`InvokeStream` for incremental SSE delivery. A stream must be consumed with
`Recv` through `io.EOF` so the terminal event and sequence can be validated.

Router errors are accepted only when their media type, v4 Platform Error
shape, trace header, HTTP status, and error code agree. The SDK exposes safe
status/code/correlation fields through `RouterError`; it never exposes raw
error response bytes.
