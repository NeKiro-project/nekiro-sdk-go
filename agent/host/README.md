# Agent Runtime host

`host` owns the process lifecycle around one managed Agent Runtime. It starts
the HTTP server, establishes and observes an optional registration lease, and
performs bounded graceful shutdown and deregistration.

It does not load deployment configuration, construct registry providers,
validate Router credentials, or implement Agent behavior. Callers must provide
those dependencies explicitly before creating the host.

```go
runtimeHost, err := host.New(host.Config{
	Address:         listenAddress,
	Handler:         managedA2AHandler,
	Registration:    registration,
	ShutdownTimeout: 5 * time.Second,
	Signals:         []os.Signal{os.Interrupt, syscall.SIGTERM},
})
if err != nil {
	return err
}
return runtimeHost.Run(context.Background())
```

Host errors expose one lifecycle `Stage` and preserve the underlying typed
error for `errors.Is` and `errors.As`. Their default message contains only the
stage and a static safe description; it never copies a provider response,
credential, request payload, or arbitrary cause text.

```go
if stage, ok := host.StageOf(err); ok {
	logger.Error("Runtime stopped", "stage", stage)
}
```

There is no retry, alternate endpoint, stale lease, or shutdown-timeout
fallback. A caller that wants any such policy must own and document it outside
this package.
