package e2e

import (
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/efficientgo/tools/core/pkg/backoff"
	"github.com/efficientgo/tools/core/pkg/errcapture"
	"github.com/pkg/errors"
)

// EnvironmentOption defined the signature of a function used to manipulate options.
type EnvironmentOption func(*environmentOptions)

type environmentOptions struct {
	envName string
	logger  Logger
}

// WithEnvironmentName tells scenario to use custom environment name instead of UUID.
// Prefer reusing names so no hanging environments are registered.
func WithEnvironmentName(envName string) EnvironmentOption {
	return func(o *environmentOptions) {
		o.envName = envName
	}
}

// WithLogger tells scenario to use custom logger to default one (stdout).
func WithLogger(logger Logger) EnvironmentOption {
	return func(o *environmentOptions) {
		o.logger = logger
	}
}

// Environment defines how to run Runnable in isolated area e.g via docker in isolated docker network.
type Environment interface {
	SharedDir() string

	// Runnable returns instance of runnable which can be started and stopped within this environment.
	Runnable(opts StartOptions) Runnable

	// Close shutdowns isolated environment and cleans it's resources.
	Close()
}

type StartOptions struct {
	Name         string
	Image        string
	EnvVars      map[string]string
	User         string
	Command      *Command
	NetworkPorts map[string]int
	Readiness    ReadinessProbe
	// WaitReadyBackoff represents backoff used for WaitReady.
	WaitReadyBackoff *backoff.Config
}

// Runnable is the entity that environment returns to manage single instance.
type Runnable interface {
	// Name returns unique name for the Runnable instance.
	Name() string

	// HostDir returns host working directory path for this runnable.
	HostDir() string
	// LocalDir returns local working directory path for this runnable.
	LocalDir() string

	// Start tells Runnable to start.
	Start() error

	// WaitReady waits until the Runnable is ready. It should return error if runnable is stopped in mean time or
	// it was stopped before.
	WaitReady() error

	// Kill tells Runnable to get killed immediately.
	// It should be ok to Stop and Kill more than once, with next invokes being noop.
	Kill() error

	// Stop tells Runnable to get gracefully stopped.
	// It should be ok to Stop and Kill more than once, with next invokes being noop.
	Stop() error

	// Exec runs the provided command inside the same process context (e.g in the docker container).
	// It returns the stdout, stderr, and error response from attempting to run the command.
	Exec(command *Command) (string, string, error)

	// Endpoint returns external (from host perspective) service endpoint (host:port) for given port name.
	// External means that it will be accessible only from host, but not from docker containers.
	//
	// If your service is not running, this method returns incorrect `stopped` endpoint.
	Endpoint(portName string) string

	// NetworkEndpoint returns internal service endpoint (host:port) for given internal port.
	// Internal means that it will be accessible only from docker containers within the network that this
	// service is running in. If you configure your local resolver with docker DNS namespace you can access it from host
	// as well. Use `Endpoint` for host access.
	//
	// If your service is not running, use `NetworkEndpointFor` instead.
	NetworkEndpoint(portName string) string

	// NetworkEndpointFor returns internal service endpoint (host:port) for given internal port and network.
	// Internal means that it will be accessible only from docker containers within the given network. If you configure
	// your local resolver with docker DNS namespace you can access it from host as well.
	//
	// This method return correct endpoint for the service in any state.
	NetworkEndpointFor(networkName string, portName string) string
}

func StartAndWaitReady(runnables ...Runnable) error {
	for _, r := range runnables {
		if err := r.Start(); err != nil {
			return err
		}
	}
	for _, r := range runnables {
		if err := r.WaitReady(); err != nil {
			return err
		}
	}
	return nil
}

type Command struct {
	Cmd                string
	Args               []string
	EntrypointDisabled bool
}

func NewCommand(cmd string, args ...string) *Command {
	return &Command{
		Cmd:  cmd,
		Args: args,
	}
}

func NewCommandWithoutEntrypoint(cmd string, args ...string) *Command {
	return &Command{
		Cmd:                cmd,
		Args:               args,
		EntrypointDisabled: true,
	}
}

type ReadinessProbe interface {
	Ready(runnable Runnable) (err error)
}

// HTTPReadinessProbe checks readiness by making HTTP call and checking for expected HTTP status code.
type HTTPReadinessProbe struct {
	portName                 string
	path                     string
	expectedStatusRangeStart int
	expectedStatusRangeEnd   int
	expectedContent          []string
}

func NewHTTPReadinessProbe(portName string, path string, expectedStatusRangeStart, expectedStatusRangeEnd int, expectedContent ...string) *HTTPReadinessProbe {
	return &HTTPReadinessProbe{
		portName:                 portName,
		path:                     path,
		expectedStatusRangeStart: expectedStatusRangeStart,
		expectedStatusRangeEnd:   expectedStatusRangeEnd,
		expectedContent:          expectedContent,
	}
}

func (p *HTTPReadinessProbe) Ready(runnable Runnable) (err error) {
	endpoint := runnable.Endpoint(p.portName)
	if endpoint == "" {
		return errors.Errorf("cannot get service endpoint for port %s", p.portName)
	}
	if endpoint == "stopped" {
		return errors.New("service has stopped")
	}

	res, err := (&http.Client{Timeout: 1 * time.Second}).Get("http://" + endpoint + p.path)
	if err != nil {
		return err
	}

	defer errcapture.ExhaustClose(&err, res.Body, "response readiness")
	body, _ := ioutil.ReadAll(res.Body)

	if res.StatusCode < p.expectedStatusRangeStart || res.StatusCode > p.expectedStatusRangeEnd {
		return errors.Errorf("expected code in range: [%v, %v], got status code: %v and body: %v", p.expectedStatusRangeStart, p.expectedStatusRangeEnd, res.StatusCode, string(body))
	}

	for _, expected := range p.expectedContent {
		if !strings.Contains(string(body), expected) {
			return errors.Errorf("expected body containing %s, got: %v", expected, string(body))
		}
	}
	return nil
}

// TCPReadinessProbe checks readiness by ensure a TCP connection can be established.
type TCPReadinessProbe struct {
	portName string
}

func NewTCPReadinessProbe(portName string) *TCPReadinessProbe {
	return &TCPReadinessProbe{
		portName: portName,
	}
}

func (p *TCPReadinessProbe) Ready(runnable Runnable) (err error) {
	endpoint := runnable.Endpoint(p.portName)
	if endpoint == "" {
		return errors.Errorf("cannot get service endpoint for port %s", p.portName)
	} else if endpoint == "stopped" {
		return errors.New("service has stopped")
	}

	conn, err := net.DialTimeout("tcp", endpoint, time.Second)
	if err != nil {
		return err
	}

	return conn.Close()
}

// CmdReadinessProbe checks readiness by `Exec`ing a command (within container) which returns 0 to consider status being ready.
type CmdReadinessProbe struct {
	cmd *Command
}

func NewCmdReadinessProbe(cmd *Command) *CmdReadinessProbe {
	return &CmdReadinessProbe{cmd: cmd}
}

func (p *CmdReadinessProbe) Ready(runnable Runnable) error {
	_, _, err := runnable.Exec(p.cmd)
	return err
}
