// Package nacos composes NeKiro Core registration primitives into a public
// Runtime-facing Nacos registration lifecycle.
//
// Core remains the owner of registration, heartbeat, lease, deregistration,
// and Nacos protocol semantics. This package owns only strict deployment
// configuration, explicit HTTP/TLS transport, and agent/host composition.
package nacos
