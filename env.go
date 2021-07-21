package e2e

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/efficientgo/tools/core/pkg/backoff"
	"github.com/efficientgo/tools/core/pkg/errcapture"
	"github.com/pkg/errors"
)

type EnvironmentConstructor func(logger Logger, envName string) (Environment, error)

// Environment defines how to run Runnable in isolated area e.g via docker in isolated docker network.
type Environment interface {
	// HostDir returns host working directory path for the runnable with name "name" on this environment.
	HostDir(name string) string
	// LocalDir returns local working directory path for the runnable with name "name" on this environment.
	LocalDir(name string) string

	// Start starts runnable using given options.
	Start(opts StartOptions) (Started, error)

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

// StartedRunnable is the started entity that Scenario can manage.
// This interface has private methods to ensure that only scenario can manage it.
type StartedRunnable interface {
	// waitReady waits until the Runnable is ready. It should return error if service is stopped in mean time or
	// it was stopped before.
	waitReady() error

	// kill tells Runnable to get killed immediately.
	// It should be ok to Stop and Kill more than once, with next invokes being noop.
	kill() error

	// stop tells Runnable to get gracefully stopped.
	// It should be ok to Stop and Kill more than once, with next invokes being noop.
	stop() error
}

// Started is the started entity.
type Started interface {
	StartedRunnable

	// Exec runs the provided command against a the docker container specified by this
	// service. It returns the stdout, stderr, and error response from attempting
	// to run the command.
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

var _ Started = NotStarted{}

type NotStarted struct {
	name         string
	networkPorts map[string]int
}

func (n NotStarted) waitReady() error { return errors.Errorf("service %v not started", n.name) }

func (n NotStarted) kill() error { return nil }

func (n NotStarted) stop() error { return nil }

func (n NotStarted) Exec(_ *Command) (string, string, error) {
	return "", "", errors.Errorf("service %v not started", n.name)
}

func (n NotStarted) Endpoint(_ string) string { return "not started" }

func (n NotStarted) NetworkEndpoint(_ string) string { return "not started" }

func (n NotStarted) NetworkEndpointFor(networkName string, portName string) string {
	return dockerNetworkContainerHostPort(networkName, n.name, n.networkPorts[portName])
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
	Ready(runnable Started) (err error)
}

// HTTPReadinessProbe checks readiness by making HTTP call and checking for expected HTTP status code.
type HTTPReadinessProbe struct {
	port                     int
	path                     string
	expectedStatusRangeStart int
	expectedStatusRangeEnd   int
	expectedContent          []string
}

func NewHTTPReadinessProbe(port int, path string, expectedStatusRangeStart, expectedStatusRangeEnd int, expectedContent ...string) *HTTPReadinessProbe {
	return &HTTPReadinessProbe{
		port:                     port,
		path:                     path,
		expectedStatusRangeStart: expectedStatusRangeStart,
		expectedStatusRangeEnd:   expectedStatusRangeEnd,
		expectedContent:          expectedContent,
	}
}

func (p *HTTPReadinessProbe) Ready(service *Service) (err error) {
	endpoint := service.Endpoint(p.port)
	if endpoint == "" {
		return fmt.Errorf("cannot get service endpoint for port %d", p.port)
	} else if endpoint == "stopped" {
		return errors.New("service has stopped")
	}

	res, err := (&http.Client{Timeout: 1 * time.Second}).Get("http://" + endpoint + p.path)
	if err != nil {
		return err
	}

	defer errcapture.ExhaustClose(&err, res.Body, "response readiness")
	body, _ := ioutil.ReadAll(res.Body)

	if res.StatusCode < p.expectedStatusRangeStart || res.StatusCode > p.expectedStatusRangeEnd {
		return fmt.Errorf("expected code in range: [%v, %v], got status code: %v and body: %v", p.expectedStatusRangeStart, p.expectedStatusRangeEnd, res.StatusCode, string(body))
	}

	for _, expected := range p.expectedContent {
		if !strings.Contains(string(body), expected) {
			return fmt.Errorf("expected body containing %s, got: %v", expected, string(body))
		}
	}

	return nil
}

// TCPReadinessProbe checks readiness by ensure a TCP connection can be established.
type TCPReadinessProbe struct {
	port int
}

func NewTCPReadinessProbe(port int) *TCPReadinessProbe {
	return &TCPReadinessProbe{
		port: port,
	}
}

func (p *TCPReadinessProbe) Ready(service *Service) (err error) {
	endpoint := service.Endpoint(p.port)
	if endpoint == "" {
		return fmt.Errorf("cannot get service endpoint for port %d", p.port)
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

func (p *CmdReadinessProbe) Ready(service *Service) error {
	_, _, err := service.Exec(p.cmd)
	return err
}
