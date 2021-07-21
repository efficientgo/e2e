package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/efficientgo/tools/core/pkg/backoff"
	"github.com/pkg/errors"
)

const dockerLocalSharedDir = "/shared"

var (
	dockerPortPattern = regexp.MustCompile(`^.*:(\d+)$`)

	_ Environment = &DockerEnvironment{}
)

// DockerEnvironment defines single node docker engine that allows to run Services.
type DockerEnvironment struct {
	dir         string
	logger      Logger
	networkName string

	registered map[string]struct{}
	started    []Runnable
}

// NewDockerEnvironment creates new, isolated docker environment.
func NewDockerEnvironment(opts ...EnvironmentOption) (*DockerEnvironment, error) {
	e := environmentOptions{}
	for _, o := range opts {
		o(&e)
	}
	if e.envName == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		e.envName = fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	}
	if e.logger == nil {
		e.logger = NewLogger(os.Stdout)
	}

	d := &DockerEnvironment{
		logger:      e.logger,
		networkName: e.envName,
		registered:  map[string]struct{}{},
	}

	// Force a shutdown in order to cleanup from a spurious situation in case
	// the previous tests run didn't cleanup correctly.
	d.Close()

	dir, err := getTmpDirectory()
	if err != nil {
		return nil, err
	}
	d.dir = dir

	// Setup the docker network.
	if out, err := RunCommandAndGetOutput("docker", "network", "create", e.envName); err != nil {
		e.logger.Log(string(out))
		d.Close()
		return nil, errors.Wrapf(err, "create docker network '%s'", e.envName)
	}
	return d, nil
}

func (e *DockerEnvironment) Runnable(opts StartOptions) Runnable {
	if e.isRegistered(opts.Name) {
		return ErrRunnable{name: opts.Name, err: errors.Errorf("there is already one runnable created with the same name %v", opts.Name)}
	}

	if opts.WaitReadyBackoff == nil {
		opts.WaitReadyBackoff = &backoff.Config{
			Min:        300 * time.Millisecond,
			Max:        600 * time.Millisecond,
			MaxRetries: 50, // Sometimes the CI is slow ¯\_(ツ)_/¯.
		}
	}

	d := &dockerRunnable{
		opts:        opts,
		logger:      e.logger,
		waitBackoff: backoff.New(context.Background(), *opts.WaitReadyBackoff),
		hostPorts:   map[string]int{},
	}
	e.register(opts.Name)
	return d
}

type ErrRunnable struct {
	name string
	err  error
}

func NewErrRunnable(name string, err error) ErrRunnable {
	return ErrRunnable{
		name: name,
		err:  err,
	}
}

func (r ErrRunnable) Name() string                           { return r.name }
func (ErrRunnable) HostDir() string                          { return "" }
func (ErrRunnable) LocalDir() string                         { return "" }
func (r ErrRunnable) Start() error                           { return r.err }
func (r ErrRunnable) WaitReady() error                       { return r.err }
func (r ErrRunnable) Kill() error                            { return r.err }
func (r ErrRunnable) Stop() error                            { return r.err }
func (r ErrRunnable) Exec(*Command) (string, string, error)  { return "", "", r.err }
func (ErrRunnable) Endpoint(string) string                   { return "" }
func (ErrRunnable) NetworkEndpoint(string) string            { return "" }
func (ErrRunnable) NetworkEndpointFor(string, string) string { return "" }

func (e *DockerEnvironment) isRegistered(name string) bool {
	_, ok := e.registered[name]
	return ok
}

func (e *DockerEnvironment) register(name string) {
	e.registered[name] = struct{}{}
}

func (e *DockerEnvironment) registerStarted(r Runnable) {
	e.started = append(e.started, r)
}

func (e *DockerEnvironment) registerStopped(name string) {
	for i, r := range e.started {
		if r.Name() == name {
			e.started = append(e.started[:i], e.started[i+1:]...)
		}
	}
}

func (e *DockerEnvironment) SharedDir() string {
	return e.dir
}

func (e *DockerEnvironment) buildDockerRunArgs(opts StartOptions) []string {
	args := []string{"run", "--rm", "--net=" + e.networkName, "--name=" + dockerNetworkContainerHost(e.networkName, opts.Name), "--hostname=" + opts.Name}

	// Mount the shared/ directory into the container. We share all containers dir to each othe to allow easier scenarios.
	args = append(args, "-v", fmt.Sprintf("%s:%s:z", e.dir, dockerLocalSharedDir))

	// Environment variables
	for name, value := range opts.EnvVars {
		args = append(args, "-e", name+"="+value)
	}

	if opts.User != "" {
		args = append(args, "--user", opts.User)
	}

	// Published ports.
	for _, port := range opts.NetworkPorts {
		args = append(args, "-p", strconv.Itoa(port))
	}

	// Disable entrypoint if required.
	if opts.Command != nil && opts.Command.EntrypointDisabled {
		args = append(args, "--entrypoint", "")
	}

	args = append(args, opts.Image)
	if opts.Command != nil {
		args = append(args, opts.Command.Cmd)
		args = append(args, opts.Command.Args...)
	}
	return args
}

type dockerRunnable struct {
	opts        StartOptions
	env         *DockerEnvironment
	logger      Logger
	waitBackoff *backoff.Backoff

	// usedNetworkName is docker NetworkName used to start this container.
	// If empty it means container is stopped.
	usedNetworkName string

	// hostPorts Maps port name to dynamically binded local ports.
	hostPorts map[string]int
}

func (d *dockerRunnable) Name() string {
	return d.opts.Name
}

func (d *dockerRunnable) HostDir() string {
	return filepath.Join(d.env.dir, "data", d.Name())
}

func (d *dockerRunnable) LocalDir() string {
	return filepath.Join(dockerLocalSharedDir, "data", d.Name())
}

func (d *dockerRunnable) isRunning() bool {
	return d.usedNetworkName != ""
}

// Start starts runnable.
func (d *dockerRunnable) Start() (err error) {
	d.logger.Log("Starting", d.Name())

	// In case of any error, if the container was already created, we
	// have to cleanup removing it. We ignore the error of the "docker rm"
	// because we don't know if the container was created or not.
	defer func() {
		if err != nil {
			_, _ = RunCommandAndGetOutput("docker", "rm", "--force", d.Name())
		}
	}()

	cmd := exec.Command("docker", d.env.buildDockerRunArgs(d.opts)...)
	cmd.Stdout = &LinePrefixLogger{prefix: d.Name() + ": ", logger: d.logger}
	cmd.Stderr = &LinePrefixLogger{prefix: d.Name() + ": ", logger: d.logger}
	if err = cmd.Start(); err != nil {
		return err
	}
	d.usedNetworkName = d.env.networkName

	// Wait until the container has been started.
	if err = d.waitForRunning(); err != nil {
		return err
	}

	d.env.registerStarted(d)

	// Get the dynamic local ports mapped to the container.
	for portName, containerPort := range d.opts.NetworkPorts {
		var out []byte
		var localPort int

		out, err = RunCommandAndGetOutput("docker", "port", d.containerName(), strconv.Itoa(containerPort))
		if err != nil {
			// Catch init errors.
			if werr := d.waitForRunning(); werr != nil {
				return errors.Wrapf(werr, "failed to get mapping for port as container %s exited: %v", d.containerName(), err)
			}
			return errors.Wrapf(err, "unable to get mapping for port %d; service: %s; output: %q", containerPort, d.Name(), out)
		}

		stdout := strings.TrimSpace(string(out))
		matches := dockerPortPattern.FindStringSubmatch(stdout)
		if len(matches) != 2 {
			return errors.Errorf("unable to get mapping for port %d (output: %s); service: %s", containerPort, stdout, d.Name())
		}

		localPort, err = strconv.Atoi(matches[1])
		if err != nil {
			return errors.Wrapf(err, "unable to get mapping for port %d; service: %s", containerPort, d.Name())
		}
		d.hostPorts[portName] = localPort
	}
	d.logger.Log("Ports for container:", d.containerName(), "Port names to host ports:", d.hostPorts)
	return nil
}

func (d *dockerRunnable) Stop() error {
	if !d.isRunning() {
		return nil
	}

	d.logger.Log("Stopping", d.Name())
	if out, err := RunCommandAndGetOutput("docker", "stop", "--time=30", d.containerName()); err != nil {
		d.logger.Log(string(out))
		return err
	}
	d.usedNetworkName = ""
	d.env.registerStopped(d.Name())
	return nil
}

func (d *dockerRunnable) Kill() error {
	if !d.isRunning() {
		return nil
	}

	d.logger.Log("Killing", d.Name())

	if out, err := RunCommandAndGetOutput("docker", "kill", d.containerName()); err != nil {
		d.logger.Log(string(out))
		return err
	}

	// Wait until the container actually stopped. However, this could fail if
	// the container already exited, so we just ignore the error.
	_, _ = RunCommandAndGetOutput("docker", "wait", d.containerName())

	d.usedNetworkName = ""
	d.env.registerStopped(d.Name())
	return nil
}

// Endpoint returns external (from host perspective) service endpoint (host:port) for given port name.
// External means that it will be accessible only from host, but not from docker containers.
//
// If your service is not running, this method returns incorrect `stopped` endpoint.
func (d *dockerRunnable) Endpoint(portName string) string {
	if !d.isRunning() {
		return "stopped"
	}

	// Map the container port to the local port.
	localPort, ok := d.hostPorts[portName]
	if !ok {
		return ""
	}

	// Do not use "localhost" cause it doesn't work with the AWS DynamoDB client.
	return fmt.Sprintf("127.0.0.1:%d", localPort)
}

// NetworkEndpoint returns internal service endpoint (host:port) for given internal port.
// Internal means that it will be accessible only from docker containers within the network that this
// service is running in. If you configure your local resolver with docker DNS namespace you can access it from host
// as well. Use `Endpoint` for host access.
//
// If your service is not running, use `NetworkEndpointFor` instead.
func (d *dockerRunnable) NetworkEndpoint(portName string) string {
	if !d.isRunning() {
		return "stopped"
	}
	return d.NetworkEndpointFor(d.usedNetworkName, portName)
}

// NetworkEndpointFor returns internal service endpoint (host:port) for given internal port and network.
// Internal means that it will be accessible only from docker containers within the given network. If you configure
// your local resolver with docker DNS namespace you can access it from host as well.
//
// This method return correct endpoint for the service in any state.
func (d *dockerRunnable) NetworkEndpointFor(networkName string, portName string) string {
	// Map the port name to the container port.
	port, ok := d.opts.NetworkPorts[portName]
	if !ok {
		return ""
	}

	return dockerNetworkContainerHostPort(networkName, d.Name(), port)
}

// dockerNetworkContainerHost return the host address of a container within the network.
func dockerNetworkContainerHost(networkName, containerName string) string {
	return fmt.Sprintf("%s-%s", networkName, containerName)
}

// dockerNetworkContainerHostPort return the host:port address of a container within the network.
func dockerNetworkContainerHostPort(networkName, containerName string, port int) string {
	return fmt.Sprintf("%s:%d", dockerNetworkContainerHost(networkName, containerName), port)
}

func (d *dockerRunnable) Ready() error {
	if !d.isRunning() {
		return fmt.Errorf("service %s is stopped", d.Name())
	}

	// Ensure the service has a readiness probe configure.
	if d.opts.Readiness == nil {
		return nil
	}

	return d.opts.Readiness.Ready(d)
}

func (d *dockerRunnable) containerName() string {
	return dockerNetworkContainerHost(d.usedNetworkName, d.Name())
}

func (d *dockerRunnable) waitForRunning() (err error) {
	if !d.isRunning() {
		return errors.Errorf("service %s is stopped", d.Name())
	}

	for d.waitBackoff.Reset(); d.waitBackoff.Ongoing(); {
		// Enforce a timeout on the command execution because we've seen some flaky tests
		// stuck here.

		var out []byte
		out, err = RunCommandWithTimeoutAndGetOutput(
			5*time.Second,
			"docker",
			"inspect",
			"--format={{json .State.Running}}",
			d.containerName(),
		)
		if err != nil {
			d.waitBackoff.Wait()
			continue
		}

		if out == nil {
			err = errors.Errorf("nil output")
			d.waitBackoff.Wait()
			continue
		}

		str := strings.TrimSpace(string(out))
		if str != "true" {
			err = errors.Errorf("unexpected output: %q", str)
			d.waitBackoff.Wait()
			continue
		}

		return nil
	}

	return errors.Wrapf(err, "docker container %s failed to start", d.Name())
}

func (d *dockerRunnable) WaitReady() (err error) {
	if !d.isRunning() {
		return errors.Errorf("service %s is stopped", d.Name())
	}

	for d.waitBackoff.Reset(); d.waitBackoff.Ongoing(); {
		err = d.Ready()
		if err == nil {
			return nil
		}

		d.waitBackoff.Wait()
	}

	return errors.Wrapf(err, "the service %s is not ready", d.Name())
}

// Exec runs the provided command against a the docker container specified by this
// service. It returns the stdout, stderr, and error response from attempting
// to run the command.
func (d *dockerRunnable) Exec(command *Command) (string, string, error) {
	if !d.isRunning() {
		return "", "", errors.Errorf("service %s is stopped", d.Name())
	}

	args := []string{"exec", d.containerName()}
	args = append(args, command.Cmd)
	args = append(args, command.Args...)

	cmd := exec.Command("docker", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func existDockerNetwork(logger Logger, networkName string) (bool, error) {
	out, err := RunCommandAndGetOutput("docker", "network", "ls", "--quiet", "--filter", fmt.Sprintf("name=%s", networkName))
	if err != nil {
		logger.Log(string(out))
		logger.Log("Unable to check if docker network", networkName, "exists:", err.Error())
		return false, err
	}

	return strings.TrimSpace(string(out)) != "", nil
}

// getTmpDirectory creates a temporary directory for shared integration
// test files, either in the working directory or a directory referenced by
// the E2E_TEMP_DIR environment variable.
func getTmpDirectory() (string, error) {
	var (
		dir string
		err error
	)
	// If a temp dir is referenced, return that.
	if os.Getenv("E2E_TEMP_DIR") != "" {
		dir = os.Getenv("E2E_TEMP_DIR")
	} else {
		dir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}

	tmpDir, err := ioutil.TempDir(dir, "e2e")
	if err != nil {
		return "", err
	}
	absDir, err := filepath.Abs(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", err
	}

	return absDir, nil
}

func (e *DockerEnvironment) Close() {
	if e == nil {
		return
	}

	if e.dir != "" {
		if err := os.RemoveAll(e.dir); err != nil {
			e.logger.Log("error while removing sharedDir", e.dir, "err:", err)
		}
	}

	// Kill the services in the opposite order.
	for i := len(e.started) - 1; i >= 0; i-- {
		if err := e.started[i].Kill(); err != nil {
			e.logger.Log("Unable to kill service", e.started[i].Name(), ":", err.Error())
		}
	}

	// Ensure there are no leftover containers.
	if out, err := RunCommandAndGetOutput(
		"docker",
		"ps",
		"-a",
		"--quiet",
		"--filter",
		fmt.Sprintf("network=%s", e.networkName),
	); err == nil {
		for _, containerID := range strings.Split(string(out), "\n") {
			containerID = strings.TrimSpace(containerID)
			if containerID == "" {
				continue
			}

			if out, err = RunCommandAndGetOutput("docker", "rm", "--force", containerID); err != nil {
				e.logger.Log(string(out))
				e.logger.Log("Unable to cleanup leftover container", containerID, ":", err.Error())
			}
		}
	} else {
		e.logger.Log(string(out))
		e.logger.Log("Unable to cleanup leftover containers:", err.Error())
	}

	// Teardown the docker network. In case the network does not exists (ie. this function
	// is called during the setup of the scenario) we skip the removal in order to not log
	// an error which may be misleading.
	if ok, err := existDockerNetwork(e.logger, e.networkName); ok || err != nil {
		if out, err := RunCommandAndGetOutput("docker", "network", "rm", e.networkName); err != nil {
			e.logger.Log(string(out))
			e.logger.Log("Unable to remove docker network", e.networkName, ":", err.Error())
		}
	}
}
