package host

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"time"
)

// Registration is the minimal lifecycle contract needed by a managed Agent
// host. Implementations own the provider-specific registration protocol.
type Registration interface {
	Register(context.Context) error
	Run(context.Context) error
	Deregister(context.Context) error
}

// Server is implemented by *http.Server and can be replaced with a fake in
// tests.
type Server interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

// ServerFactory allows tests and embedders to provide a server implementation.
// The default factory creates an *http.Server from Address and Handler.
type ServerFactory func(address string, handler http.Handler) Server

// Config contains only explicit host policy. Address, handler, timeout, and
// signal behavior are required. An empty Signals slice explicitly disables
// process-signal handling, which is useful when the caller owns cancellation.
type Config struct {
	Address         string
	Handler         http.Handler
	Registration    Registration
	ShutdownTimeout time.Duration
	Signals         []os.Signal
	NewServer       ServerFactory
}

// Host owns one Runtime process lifecycle.
type Host struct {
	config Config
}

// New validates explicit host policy and returns a lifecycle runner.
func New(config Config) (*Host, error) {
	if config.Address == "" || isNil(config.Handler) || config.ShutdownTimeout <= 0 || config.Signals == nil || config.Registration != nil && isNil(config.Registration) {
		return nil, Wrap(StageConfig, "validate host policy", errors.New("address, handler, shutdown timeout, and signals are required"))
	}
	for _, configuredSignal := range config.Signals {
		if configuredSignal == nil {
			return nil, Wrap(StageConfig, "validate host policy", errors.New("signals cannot contain nil"))
		}
	}
	if config.NewServer == nil {
		config.NewServer = func(address string, handler http.Handler) Server {
			return &http.Server{Addr: address, Handler: handler}
		}
	}
	return &Host{config: config}, nil
}

// Run starts the HTTP server, observes registration, and performs graceful
// shutdown. A context cancellation is a normal lifecycle completion; server,
// lease, and shutdown failures remain staged errors.
func (host *Host) Run(ctx context.Context) error {
	if host == nil || ctx == nil {
		return Wrap(StageConfig, "run host", errors.New("host and context are required"))
	}
	runContext := ctx
	if len(host.config.Signals) > 0 {
		var stop context.CancelFunc
		runContext, stop = signal.NotifyContext(ctx, host.config.Signals...)
		defer stop()
	}
	runContext, cancel := context.WithCancel(runContext)
	defer cancel()

	server := host.config.NewServer(host.config.Address, host.config.Handler)
	if server == nil {
		return Wrap(StageConfig, "create server", errors.New("server factory returned nil"))
	}
	if host.config.Registration != nil {
		if err := host.config.Registration.Register(runContext); err != nil {
			return Wrap(StageRegistration, "register runtime", err)
		}
	}

	serverErrors := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverErrors <- err
	}()

	registrationErrors := make(chan error, 1)
	if host.config.Registration != nil {
		go func() { registrationErrors <- host.config.Registration.Run(runContext) }()
	}

	var runErr error
	serverStopped := false
	registrationStopped := host.config.Registration == nil
	select {
	case <-runContext.Done():
	case err := <-serverErrors:
		serverStopped = true
		if err == nil && runContext.Err() == nil {
			err = errors.New("server stopped before host cancellation")
		}
		if err != nil && runContext.Err() == nil {
			runErr = Wrap(StageServe, "serve runtime", err)
		}
	case err := <-registrationErrors:
		registrationStopped = true
		if err == nil && runContext.Err() == nil {
			err = errors.New("registration lease stopped before host cancellation")
		}
		if err != nil && runContext.Err() == nil {
			runErr = Wrap(StageRegistration, "registration lease", err)
		}
	}
	cancel()

	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), host.config.ShutdownTimeout)
	defer shutdownCancel()
	shutdownErr := server.Shutdown(shutdownContext)
	if shutdownErr != nil {
		runErr = errors.Join(runErr, Wrap(StageShutdown, "shutdown server", shutdownErr))
	}
	if !serverStopped {
		select {
		case err := <-serverErrors:
			if err != nil {
				runErr = errors.Join(runErr, Wrap(StageShutdown, "stop server", err))
			}
		case <-shutdownContext.Done():
			runErr = errors.Join(runErr, Wrap(StageShutdown, "stop server", shutdownContext.Err()))
		}
	}
	if !registrationStopped && host.config.Registration != nil {
		select {
		case err := <-registrationErrors:
			if err != nil && !errors.Is(err, context.Canceled) {
				runErr = errors.Join(runErr, Wrap(StageRegistration, "stop registration lease", err))
			}
		case <-shutdownContext.Done():
			runErr = errors.Join(runErr, Wrap(StageShutdown, "stop registration lease", shutdownContext.Err()))
		}
	}
	if host.config.Registration != nil {
		if err := host.config.Registration.Deregister(shutdownContext); err != nil {
			runErr = errors.Join(runErr, Wrap(StageShutdown, "deregister runtime", err))
		}
	}
	return runErr
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
