package nacos

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/NeKiro-project/NeKiro/registry"
	registrynacos "github.com/NeKiro-project/NeKiro/registry/nacos"
)

type requestExecutor = registrynacos.RequestExecutor

// Registration is one exact Runtime instance lease.
type Registration struct {
	registrar    registry.InstanceRegistrar
	registration registry.Registration

	mu    sync.RWMutex
	lease registry.InstanceLease
}

// New constructs a registration and its explicit HTTP/TLS transport.
func New(config Config) (*Registration, error) {
	executor, err := newHTTPClient(config)
	if err != nil {
		return nil, err
	}
	return newWithExecutor(config, executor)
}

func newWithExecutor(config Config, executor requestExecutor) (*Registration, error) {
	if config.Mode != ModeNacos || executor == nil || config.Validate() != nil {
		return nil, errors.New("Nacos registration dependencies are invalid")
	}
	target, _ := registry.NewReleaseTarget(registry.ReleaseTargetInput{
		AgentID: config.AgentID, AgentCardVersion: config.AgentCardVersion, ReleaseID: config.ReleaseID,
		CardDigest: config.CardDigest, CanonicalEndpoint: config.CanonicalEndpoint, Audience: config.Audience,
	})
	binding, err := registrynacos.NewBinding(registrynacos.BindingInput{
		Target: target, ServiceName: config.ServiceName, GroupName: config.GroupName, ClusterName: config.ClusterName,
	})
	if err != nil {
		return nil, errors.New("Nacos registration binding is invalid")
	}
	addressType := registry.AddressTypeIPv4
	if parsed := net.ParseIP(config.AdvertisedIP); parsed != nil && parsed.To4() == nil {
		addressType = registry.AddressTypeIPv6
	}
	endpoint, err := registry.NewNetworkEndpoint(registry.NetworkEndpointInput{
		AddressType: addressType, Address: config.AdvertisedIP, PortName: config.PortName,
		Port: config.AdvertisedPort, Protocol: registry.TransportProtocolTCP,
	})
	if err != nil {
		return nil, errors.New("Nacos registration endpoint is invalid")
	}
	weight := int(config.Weight)
	instance, err := registry.NewInstance(registry.InstanceInput{
		ID: config.InstanceID, Endpoints: []registry.NetworkEndpoint{endpoint}, Ready: true, Serving: true, Weight: &weight,
	})
	if err != nil {
		return nil, errors.New("Nacos registration instance is invalid")
	}
	registration, err := registry.NewRegistration(registry.RegistrationInput{Target: target, Instance: instance})
	if err != nil {
		return nil, errors.New("Nacos registration is invalid")
	}
	registrar, err := registrynacos.NewRegistrar(registrynacos.RegistrarConfig{
		APIOrigin: config.APIOrigin, NamespaceID: config.NamespaceID, Binding: binding, PortName: config.PortName,
		Weight: config.Weight, HeartbeatInterval: config.HeartbeatInterval, HeartbeatTimeout: config.HeartbeatTimeout,
		IPDeleteTimeout: config.IPDeleteTimeout, AuthMode: config.AuthMode, AccessToken: config.AccessToken, Executor: executor,
	})
	if err != nil {
		return nil, errors.New("Nacos registrar configuration is invalid")
	}
	return &Registration{registrar: registrar, registration: registration}, nil
}

// Register publishes the instance and starts the Core-owned lease.
func (value *Registration) Register(ctx context.Context) error {
	if ctx == nil {
		return errors.New("Nacos registration context is required")
	}
	value.mu.Lock()
	defer value.mu.Unlock()
	if value.lease != nil {
		return errors.New("Nacos registration has already started")
	}
	lease, err := value.registrar.Register(ctx, value.registration)
	if err != nil {
		return fmt.Errorf("register runtime with Nacos: %w", err)
	}
	value.lease = lease
	return nil
}

// Run observes lease termination until the host context is canceled.
func (value *Registration) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("Nacos registration context is required")
	}
	value.mu.RLock()
	lease := value.lease
	value.mu.RUnlock()
	if lease == nil {
		return errors.New("Nacos registration has not started")
	}
	select {
	case <-ctx.Done():
		return nil
	case <-lease.Done():
		err := lease.Err()
		if err == nil {
			err = errors.New("lease stopped without a terminal error")
		}
		return fmt.Errorf("Nacos registration lease terminated: %w", err)
	}
}

// Deregister closes the lease and the underlying registrar. Repeated calls are
// safe and retain Core's idempotent close semantics.
func (value *Registration) Deregister(ctx context.Context) error {
	if ctx == nil {
		return errors.New("Nacos deregistration context is required")
	}
	value.mu.RLock()
	lease := value.lease
	value.mu.RUnlock()
	var leaseErr error
	if lease != nil {
		leaseErr = lease.Close(ctx)
	}
	return errors.Join(leaseErr, value.registrar.Close())
}

// Ready reports whether a published lease remains active.
func (value *Registration) Ready() bool {
	value.mu.RLock()
	lease := value.lease
	value.mu.RUnlock()
	if lease == nil {
		return false
	}
	select {
	case <-lease.Done():
		return false
	default:
		return true
	}
}
