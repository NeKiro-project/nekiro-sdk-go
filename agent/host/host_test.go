package host

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

type fakeRegistration struct {
	mu             sync.Mutex
	registered     bool
	registerErr    error
	runErr         error
	deregisterErr  error
	deregistered   bool
	runStarted     chan struct{}
	allowRunReturn chan struct{}
}

func (registration *fakeRegistration) Register(context.Context) error {
	registration.mu.Lock()
	defer registration.mu.Unlock()
	if registration.registerErr == nil {
		registration.registered = true
	}
	return registration.registerErr
}

func (registration *fakeRegistration) Run(ctx context.Context) error {
	close(registration.runStarted)
	select {
	case <-ctx.Done():
		return nil
	case <-registration.allowRunReturn:
		return registration.runErr
	}
}

func (registration *fakeRegistration) Deregister(context.Context) error {
	registration.mu.Lock()
	registration.deregistered = true
	registration.mu.Unlock()
	return registration.deregisterErr
}

type fakeServer struct {
	serve       chan error
	shutdown    chan struct{}
	shutdownErr error
}

func (server *fakeServer) ListenAndServe() error {
	select {
	case err := <-server.serve:
		return err
	case <-server.shutdown:
		return http.ErrServerClosed
	}
}

func (server *fakeServer) Shutdown(context.Context) error {
	select {
	case <-server.shutdown:
	default:
		close(server.shutdown)
	}
	return server.shutdownErr
}

func TestRunRegistersServesAndDeregistersOnCancellation(t *testing.T) {
	registration := &fakeRegistration{runStarted: make(chan struct{}), allowRunReturn: make(chan struct{})}
	server := &fakeServer{serve: make(chan error, 1), shutdown: make(chan struct{})}
	host, err := New(Config{
		Address: "127.0.0.1:0", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		Registration: registration, ShutdownTimeout: time.Second,
		Signals: []os.Signal{}, NewServer: func(string, http.Handler) Server { return server },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- host.Run(ctx) }()
	<-registration.runStarted
	cancel()
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error=%v", err)
	}
	if !registration.registered {
		t.Fatal("registration was not started")
	}
	if !registration.deregistered {
		t.Fatal("registration was not removed")
	}
	select {
	case <-server.shutdown:
	default:
		t.Fatal("server was not shut down")
	}
}

func TestNewRequiresExplicitLifecyclePolicy(t *testing.T) {
	valid := Config{
		Address: "127.0.0.1:0", Handler: http.NotFoundHandler(), ShutdownTimeout: time.Second,
		Signals: []os.Signal{},
	}
	for name, mutate := range map[string]func(*Config){
		"address":    func(config *Config) { config.Address = "" },
		"handler":    func(config *Config) { config.Handler = nil },
		"timeout":    func(config *Config) { config.ShutdownTimeout = 0 },
		"signals":    func(config *Config) { config.Signals = nil },
		"nil signal": func(config *Config) { config.Signals = []os.Signal{nil} },
		"server":     func(config *Config) { config.NewServer = func(string, http.Handler) Server { return nil } },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			host, err := New(candidate)
			if name == "server" {
				if err != nil {
					t.Fatal(err)
				}
				err = host.Run(t.Context())
			}
			if err == nil {
				t.Fatal("invalid policy was accepted")
			}
			if stage, ok := StageOf(err); !ok || stage != StageConfig {
				t.Fatalf("StageOf() = %q, %v", stage, ok)
			}
		})
	}
	var typedNil *fakeRegistration
	valid.Registration = typedNil
	if _, err := New(valid); err == nil {
		t.Fatal("typed-nil registration was accepted")
	}
}

func TestRunStagesRegistrationFailure(t *testing.T) {
	want := errors.New("registration failed")
	registration := &fakeRegistration{registerErr: want, runStarted: make(chan struct{}), allowRunReturn: make(chan struct{})}
	host, err := New(Config{
		Address: "127.0.0.1:0", Handler: http.NotFoundHandler(),
		Registration: registration, ShutdownTimeout: time.Second,
		Signals: []os.Signal{},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = host.Run(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("Run() error=%v does not preserve registration cause", err)
	}
	if stage, ok := StageOf(err); !ok || stage != StageRegistration {
		t.Fatalf("StageOf() = %q, %v", stage, ok)
	}
}

func TestRunStagesServerFailureAndDeregistrationFailure(t *testing.T) {
	serverErr := errors.New("server failed")
	deregisterErr := errors.New("deregister failed")
	registration := &fakeRegistration{runStarted: make(chan struct{}), allowRunReturn: make(chan struct{}), deregisterErr: deregisterErr}
	server := &fakeServer{serve: make(chan error, 1), shutdown: make(chan struct{})}
	server.serve <- serverErr
	host, err := New(Config{
		Address: "127.0.0.1:0", Handler: http.NotFoundHandler(), Registration: registration,
		ShutdownTimeout: time.Second, Signals: []os.Signal{},
		NewServer: func(string, http.Handler) Server { return server },
	})
	if err != nil {
		t.Fatal(err)
	}
	err = host.Run(context.Background())
	if !errors.Is(err, serverErr) || !errors.Is(err, deregisterErr) {
		t.Fatalf("Run() error=%v does not preserve joined causes", err)
	}
	if stage, ok := StageOf(err); !ok || stage != StageServe {
		t.Fatalf("StageOf() = %q, %v", stage, ok)
	}
}

func TestRunStagesLeaseFailure(t *testing.T) {
	want := errors.New("lease failed")
	registration := &fakeRegistration{
		runStarted: make(chan struct{}), allowRunReturn: make(chan struct{}), runErr: want,
	}
	server := &fakeServer{serve: make(chan error, 1), shutdown: make(chan struct{})}
	host, err := New(Config{
		Address: "127.0.0.1:0", Handler: http.NotFoundHandler(), Registration: registration,
		ShutdownTimeout: time.Second, Signals: []os.Signal{},
		NewServer: func(string, http.Handler) Server { return server },
	})
	if err != nil {
		t.Fatal(err)
	}
	close(registration.allowRunReturn)
	err = host.Run(t.Context())
	if !errors.Is(err, want) {
		t.Fatalf("Run() error=%v does not preserve lease cause", err)
	}
	if stage, ok := StageOf(err); !ok || stage != StageRegistration {
		t.Fatalf("StageOf() = %q, %v", stage, ok)
	}
}

type blockingRegistration struct {
	runStarted   chan struct{}
	stop         chan struct{}
	deregistered bool
}

func (*blockingRegistration) Register(context.Context) error { return nil }
func (registration *blockingRegistration) Run(context.Context) error {
	close(registration.runStarted)
	<-registration.stop
	return nil
}
func (registration *blockingRegistration) Deregister(context.Context) error {
	registration.deregistered = true
	close(registration.stop)
	return nil
}

func TestRunStagesShutdownTimeoutWhenLeaseDoesNotStop(t *testing.T) {
	registration := &blockingRegistration{runStarted: make(chan struct{}), stop: make(chan struct{})}
	server := &fakeServer{serve: make(chan error, 1), shutdown: make(chan struct{})}
	host, err := New(Config{
		Address: "127.0.0.1:0", Handler: http.NotFoundHandler(), Registration: registration,
		ShutdownTimeout: 10 * time.Millisecond, Signals: []os.Signal{},
		NewServer: func(string, http.Handler) Server { return server },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- host.Run(ctx) }()
	<-registration.runStarted
	cancel()
	err = <-done
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error=%v does not preserve shutdown timeout", err)
	}
	if stage, ok := StageOf(err); !ok || stage != StageShutdown {
		t.Fatalf("StageOf() = %q, %v", stage, ok)
	}
}
