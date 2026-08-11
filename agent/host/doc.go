// Package host provides the lifecycle boundary for a managed Agent Runtime.
//
// It owns bootstrap-stage error context, HTTP serving, registration lease
// observation, and graceful shutdown. It does not own an Agent's model,
// prompt, tool, workflow, memory, or Session implementation.
package host
