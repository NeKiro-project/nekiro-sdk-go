# Public Nacos registration

`agent/registration/nacos` publishes one exact NeKiro Agent Release and Runtime
instance through a Nacos registration lease. It is the public composition layer
for provider processes that run under `agent/host`.

```go
config, err := nacos.LoadConfig(os.LookupEnv, "MY_AGENT", agentID, instanceID)
if err != nil {
    return err
}
var registration agenthost.Registration
if config.Mode == nacos.ModeNacos {
    registration, err = nacos.New(config)
    if err != nil {
        return err
    }
}
```

The prefix selects explicit environment names such as
`MY_AGENT_REGISTRATION_MODE` and `MY_AGENT_NACOS_API_ORIGIN`. Disabled mode
rejects every Nacos-specific variable. Nacos mode requires the exact Release,
advertised endpoint, freshness, authentication, and transport policy. HTTPS
requires a private CA and server name; mTLS requires a complete client
certificate/key pair. Redirects, ambient proxies, system roots, retry, stale
success, and fallback endpoints are disabled.

NeKiro Core owns registration, heartbeat, lease, deregistration, and Nacos
protocol semantics. This package does not persist Agent Cards or Releases,
discover providers, select endpoints, or implement Agent behavior.
