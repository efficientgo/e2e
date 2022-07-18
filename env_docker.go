// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/efficientgo/tools/core/pkg/backoff"
	"github.com/pkg/errors"
)

const (
	dockerLocalSharedDir   = "/shared"
	dockerMacOSGatewayAddr = "gateway.docker.internal"
)

var (
	dockerPortPattern = regexp.MustCompile(`^.*:(\d+)`)

	_ Environment = &DockerEnvironment{}
)

// DockerEnvironment defines single node docker engine that allows to run Services.
type DockerEnvironment struct {
	dir         string
	logger      Logger
	networkName string

	hostAddr string

	registered map[string]struct{}
	listeners  []EnvironmentListener
	started    []Runnable

	verbose bool
	closers []func()
	closed  bool
}

// NewDockerEnvironment creates new, isolated docker environment.
func NewDockerEnvironment(name string, opts ...EnvironmentOption) (*DockerEnvironment, error) {
	e := environmentOptions{}
	for _, o := range opts {
		o(&e)
	}
	if name == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		name = fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	}
	if e.logger == nil {
		e.logger = NewLogger(os.Stdout)
	}

	d := &DockerEnvironment{
		logger:      e.logger,
		networkName: name,
		verbose:     e.verbose,
		registered:  map[string]struct{}{},
	}

	// Force a shutdown in order to cleanup from a spurious situation in case
	// the previous tests run didn't cleanup correctly.
	d.close()

	dir, err := getTmpDirectory()
	if err != nil {
		return nil, err
	}
	d.dir = dir

	// Setup the docker network.
	if out, err := d.exec("docker", "network", "create", "-d", "bridge", name).CombinedOutput(); err != nil {
		e.logger.Log(string(out))
		d.Close()
		return nil, errors.Wrapf(err, "create docker network '%s'", name)
	}

	switch runtime.GOOS {
	case "darwin":
		d.hostAddr = dockerMacOSGatewayAddr
	default:
		out, err := d.exec("docker", "network", "inspect", name).CombinedOutput()
		if err != nil {
			e.logger.Log(string(out))
			d.Close()
			return nil, errors.Wrapf(err, "inspect docker network '%s'", name)
		}

		var inspectDetails []struct {
			IPAM struct {
				Config []struct {
					Gateway string `json:"Gateway"`
				} `json:"Config"`
			} `json:"IPAM"`
		}
		if err := json.Unmarshal(out, &inspectDetails); err != nil {
			return nil, errors.Wrap(err, "unmarshall docker inspect details to obtain Gateway IP")
		}

		if len(inspectDetails) != 1 || len(inspectDetails[0].IPAM.Config) != 1 {
			return nil, errors.Errorf("unexpected format of docker inspect; expected exactly one element in root and IPAM.Config, got %v", string(out))
		}
		d.hostAddr = inspectDetails[0].IPAM.Config[0].Gateway
	}

	return d, nil
}

func (e *DockerEnvironment) HostAddr() string { return e.hostAddr }
func (e *DockerEnvironment) Name() string     { return e.networkName }

func (e *DockerEnvironment) AddCloser(f func()) {
	e.closers = append(e.closers, f)
}

func (e *DockerEnvironment) Runnable(name string) RunnableBuilder {
	if e.closed {
		return Errorer{name: name, err: errors.New("environment close was invoked already.")}
	}

	if e.isRegistered(name) {
		return Errorer{name: name, err: errors.Errorf("there is already one runnable created with the same name %v", name)}
	}

	d := &dockerRunnable{
		env:       e,
		name:      name,
		logger:    e.logger,
		ports:     map[string]int{},
		hostPorts: map[string]int{},
	}
	d.concreteType = d
	if err := os.MkdirAll(d.Dir(), 0750); err != nil {
		return Errorer{name: name, err: err}
	}
	e.register(name)
	return d
}

// AddListener registers given listener to be notified on environment runnable changes.
func (e *DockerEnvironment) AddListener(listener EnvironmentListener) {
	e.listeners = append(e.listeners, listener)
}

type Errorer struct {
	name string
	err  error
}

func NewErrorer(name string, err error) Errorer {
	return Errorer{
		name: name,
		err:  err,
	}
}

func (e Errorer) id() uintptr                               { return 0 }
func (e Errorer) Name() string                              { return e.name }
func (Errorer) Dir() string                                 { return "" }
func (Errorer) InternalDir() string                         { return "" }
func (e Errorer) Start() error                              { return e.err }
func (e Errorer) WaitReady() error                          { return e.err }
func (e Errorer) Kill() error                               { return e.err }
func (e Errorer) Stop() error                               { return e.err }
func (e Errorer) Exec(Command, ...ExecOption) error         { return e.err }
func (Errorer) Endpoint(string) string                      { return "" }
func (Errorer) InternalEndpoint(string) string              { return "" }
func (Errorer) IsRunning() bool                             { return false }
func (e Errorer) Init(StartOptions) Runnable                { return e }
func (e Errorer) WithPorts(map[string]int) RunnableBuilder  { return e }
func (e Errorer) WithConcreteType(Runnable) RunnableBuilder { return e }
func (e Errorer) Future() FutureRunnable                    { return e }

func (e *DockerEnvironment) isRegistered(name string) bool {
	_, ok := e.registered[name]
	return ok
}

func (e *DockerEnvironment) register(name string) {
	e.registered[name] = struct{}{}
}

func (e *DockerEnvironment) registerStarted(r Runnable) error {
	e.started = append(e.started, r)

	for _, l := range e.listeners {
		if err := l.OnRunnableChange(e.started); err != nil {
			return err
		}
	}
	return nil
}

func (e *DockerEnvironment) registerStopped(name string) error {
	for i, r := range e.started {
		if r.Name() == name {
			e.started = append(e.started[:i], e.started[i+1:]...)
			for _, l := range e.listeners {
				if err := l.OnRunnableChange(e.started); err != nil {
					return err
				}
			}
			return nil
		}
	}
	return nil
}

func (e *DockerEnvironment) SharedDir() string {
	return e.dir
}

func (e *DockerEnvironment) buildDockerRunArgs(name string, ports map[string]int, opts StartOptions) []string {
	args := []string{"--rm", "--net=" + e.networkName, "--name=" + dockerNetworkContainerHost(e.networkName, name), "--hostname=" + name}

	// Mount the shared/ directory into the container. We share all containers dir to each other to allow easier scenarios.
	args = append(args, "-v", fmt.Sprintf("%s:%s:z", e.dir, dockerLocalSharedDir))

	for _, v := range opts.Volumes {
		args = append(args, "-v", v)
	}

	// Environment variables
	for name, value := range opts.EnvVars {
		args = append(args, "-e", name+"="+value)
	}

	if opts.User != "" {
		args = append(args, "--user", opts.User)
	}

	if opts.UserNs != "" {
		args = append(args, "--userns", opts.UserNs)
	}

	if opts.Privileged {
		args = append(args, "--privileged")
	}

	for _, c := range opts.Capabilities {
		args = append(args, "--cap-add", string(c))
	}

	if opts.LimitMemoryBytes > 0 {
		args = append(args, "--memory", fmt.Sprintf("%db", opts.LimitMemoryBytes))
	}

	if opts.LimitCPUs > 0 {
		args = append(args, "--cpus", fmt.Sprintf("%f", opts.LimitCPUs))
	}

	// Published ports.
	for _, port := range ports {
		args = append(args, "-p", strconv.Itoa(port))
	}

	// Disable entrypoint if required.
	if opts.Command.EntrypointDisabled {
		args = append(args, "--entrypoint", "")
	}

	args = append(args, opts.Image)
	if opts.Command.Cmd != "" {
		args = append(args, opts.Command.Cmd)
	}
	if len(opts.Command.Args) > 0 {
		args = append(args, opts.Command.Args...)
	}
	return args
}

type dockerRunnable struct {
	env   *DockerEnvironment
	name  string
	ports map[string]int

	logger           Logger
	opts             StartOptions
	waitBackoffReady *backoff.Backoff

	// usedNetworkName is docker NetworkName used to start this container.
	// If empty it means container is stopped.
	usedNetworkName string

	// hostPorts Maps port name to dynamically binded local ports.
	hostPorts map[string]int

	concreteType Runnable
}

func (d *dockerRunnable) Name() string {
	return d.name
}

func (d *dockerRunnable) Dir() string {
	return filepath.Join(d.env.dir, "data", d.Name())
}

func (d *dockerRunnable) InternalDir() string {
	return filepath.Join(dockerLocalSharedDir, "data", d.Name())
}

func (d *dockerRunnable) Init(opts StartOptions) Runnable {
	if opts.WaitReadyBackoff == nil {
		opts.WaitReadyBackoff = &backoff.Config{
			Min:        300 * time.Millisecond,
			Max:        600 * time.Millisecond,
			MaxRetries: 50, // Sometimes the CI is slow ¯\_(ツ)_/¯.
		}
	}

	d.opts = opts
	d.waitBackoffReady = backoff.New(context.Background(), *opts.WaitReadyBackoff)
	return d
}

func (d *dockerRunnable) WithPorts(ports map[string]int) RunnableBuilder {
	d.ports = ports
	return d
}

func (d *dockerRunnable) WithConcreteType(r Runnable) RunnableBuilder {
	d.concreteType = r
	return d
}

func (d *dockerRunnable) id() uintptr {
	return uintptr(unsafe.Pointer(d))
}

func (d *dockerRunnable) Future() FutureRunnable {
	return d
}

func (d *dockerRunnable) IsRunning() bool {
	return d.usedNetworkName != ""
}

// Start starts runnable.
func (d *dockerRunnable) Start() (err error) {
	if d.IsRunning() {
		return errors.Errorf("%v is running. Stop or kill it first to restart.", d.Name())
	}

	i, ok := d.concreteType.(identificable)
	if !ok {
		return errors.Errorf("concrete type has at least embed runnable or future runnable instance provided by Runnable builder, got %T; not implementing identificable", d.concreteType)
	}
	if i.id() != d.id() {
		return errors.Errorf("concrete type has at least embed runnable or future runnable instance provided by Runnable builder, got %T; id %v, expected %v", d.concreteType, i.id(), d.id())
	}

	d.logger.Log("Starting", d.Name())

	// In case of any error, if the container was already created, we
	// have to cleanup removing it. We ignore the error of the "docker rm"
	// because we don't know if the container was created or not.
	defer func() {
		if err != nil {
			_, _ = d.env.exec("docker", "rm", "--force", d.Name()).CombinedOutput()
		}
	}()

	// Make sure the image is available locally; if not wait for it to download.
	if err = d.prePullImage(context.TODO()); err != nil {
		return err
	}

	cmd := d.env.exec("docker", append([]string{"run"}, d.env.buildDockerRunArgs(d.name, d.ports, d.opts)...)...)
	l := &LinePrefixLogger{prefix: d.Name() + ": ", logger: d.logger}
	cmd.Stdout = l
	cmd.Stderr = l
	if err = cmd.Start(); err != nil {
		return err
	}
	d.usedNetworkName = d.env.networkName

	// Wait until the container has been started.
	if err = d.waitForRunning(); err != nil {
		return err
	}

	if err := d.env.registerStarted(d.concreteType); err != nil {
		return err
	}

	// Get the dynamic local ports mapped to the container.
	for portName, containerPort := range d.ports {
		var out []byte
		out, err = d.env.exec("docker", "port", d.containerName(), strconv.Itoa(containerPort)).CombinedOutput()
		if err != nil {
			// Catch init errors.
			if werr := d.waitForRunning(); werr != nil {
				return errors.Wrapf(werr, "failed to get mapping for port as container %s exited: %v", d.containerName(), err)
			}
			return errors.Wrapf(err, "unable to get mapping for port %d; service: %s; output: %q", containerPort, d.Name(), out)
		}

		d.hostPorts[portName], err = getDockerPortMapping(out)
		if err != nil {
			return errors.Wrapf(err, "unable to get mapping for port %d; service: %s", containerPort, d.Name())
		}
	}
	d.logger.Log("Ports for container", d.containerName(), ">> Local ports:", d.ports, "Ports available from host:", d.hostPorts)
	return nil
}

func getDockerPortMapping(out []byte) (int, error) {
	trimmed := strings.TrimSpace(string(out))
	matches := dockerPortPattern.FindStringSubmatch(trimmed)
	if len(matches) != 2 {
		return 0, errors.Errorf("got unexpected output: %s", trimmed)
	}
	return strconv.Atoi(matches[1])
}

func (d *dockerRunnable) Stop() error {
	if !d.IsRunning() {
		return nil
	}

	d.logger.Log("Stopping", d.Name())
	if out, err := d.env.exec("docker", "stop", "--time=30", d.containerName()).CombinedOutput(); err != nil {
		d.logger.Log(string(out))
		return err
	}
	d.usedNetworkName = ""
	return d.env.registerStopped(d.Name())
}

func (d *dockerRunnable) Kill() error {
	if !d.IsRunning() {
		return nil
	}

	d.logger.Log("Killing", d.Name())

	if out, err := d.env.exec("docker", "kill", d.containerName()).CombinedOutput(); err != nil {
		d.logger.Log(string(out))
		return err
	}

	// Wait until the container actually stopped. However, this could fail if
	// the container already exited, so we just ignore the error.
	_, _ = d.env.exec("docker", "wait", d.containerName()).CombinedOutput()

	d.usedNetworkName = ""
	return d.env.registerStopped(d.Name())
}

// Endpoint returns external (from host perspective) service endpoint (host:port) for given port name.
// External means that it will be accessible only from host, but not from docker containers.
//
// If your service is not running, this method returns incorrect `stopped` endpoint.
func (d *dockerRunnable) Endpoint(portName string) string {
	if !d.IsRunning() {
		return "stopped"
	}

	// Map the container port to the local port.
	localPort, ok := d.hostPorts[portName]
	if !ok {
		return ""
	}

	// Do not use "localhost", because it doesn't work with the AWS DynamoDB client.
	return fmt.Sprintf("127.0.0.1:%d", localPort)
}

// InternalEndpoint returns internal service endpoint (host:port) for given internal port.
// Internal means that it will be accessible only from docker containers within the network that this
// service is running in. If you configure your local resolver with docker DNS namespace you can access it from host
// as well. Use `Endpoint` for host access.
func (d *dockerRunnable) InternalEndpoint(portName string) string {
	// Map the port name to the container port.
	port, ok := d.ports[portName]
	if !ok {
		return ""
	}

	return dockerNetworkContainerHostPort(d.env.networkName, d.Name(), port)
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
	if !d.IsRunning() {
		return errors.Errorf("service %s is stopped", d.Name())
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
	if !d.IsRunning() {
		return errors.Errorf("service %s is stopped", d.Name())
	}

	var out []byte
	for d.waitBackoffReady.Reset(); d.waitBackoffReady.Ongoing(); {
		// Enforce a timeout on the command execution because we've seen some flaky tests
		// stuck here.

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err = d.env.execContext(
			ctx,
			"docker",
			"inspect",
			"--format={{json .State.Running}}",
			d.containerName(),
		).CombinedOutput()
		if err != nil {
			d.waitBackoffReady.Wait()
			continue
		}

		if out == nil {
			err = errors.Errorf("nil output")
			d.waitBackoffReady.Wait()
			continue
		}

		str := strings.TrimSpace(string(out))
		if str != "true" {
			err = errors.Errorf("unexpected output: %q", str)
			d.waitBackoffReady.Wait()
			continue
		}

		return nil
	}

	if len(out) > 0 {
		d.logger.Log(string(out))
	}
	return errors.Wrapf(err, "docker container %s failed to start", d.Name())
}

func (d *dockerRunnable) prePullImage(ctx context.Context) (err error) {
	if d.IsRunning() {
		return errors.Errorf("service %s is running; expected stopped", d.Name())
	}

	if _, err = d.env.execContext(ctx, "docker", "image", "inspect", d.opts.Image).CombinedOutput(); err == nil {
		return nil
	}

	// Assuming Error: No such image: <image>.
	cmd := d.env.execContext(ctx, "docker", "pull", d.opts.Image)
	l := &LinePrefixLogger{prefix: d.Name() + ": ", logger: d.logger}
	cmd.Stdout = l
	cmd.Stderr = l
	if err = cmd.Run(); err != nil {
		return errors.Wrapf(err, "docker image %s failed to download", d.opts.Image)
	}
	return nil
}

func (d *dockerRunnable) WaitReady() (err error) {
	if !d.IsRunning() {
		return errors.Errorf("service %s is stopped", d.Name())
	}

	for d.waitBackoffReady.Reset(); d.waitBackoffReady.Ongoing(); {
		err = d.Ready()
		if err == nil {
			return nil
		}

		d.waitBackoffReady.Wait()
	}
	return errors.Wrapf(err, "the service %s is not ready", d.Name())
}

// Exec runs the provided command against the docker container specified by this
// service.
func (d *dockerRunnable) Exec(command Command, opts ...ExecOption) error {
	if !d.IsRunning() {
		return errors.Errorf("service %s is stopped", d.Name())
	}

	l := &LinePrefixLogger{prefix: d.Name() + "-exec: ", logger: d.logger}
	o := ExecOptions{Stdout: l, Stderr: l}
	for _, opt := range opts {
		opt(&o)
	}

	args := []string{"exec", d.containerName()}
	args = append(args, command.Cmd)
	args = append(args, command.Args...)
	cmd := d.env.exec("docker", args...)
	cmd.Stdout = o.Stdout
	cmd.Stderr = o.Stderr
	return cmd.Run()
}

func (e *DockerEnvironment) existDockerNetwork() (bool, error) {
	out, err := e.exec("docker", "network", "ls", "--quiet", "--filter", fmt.Sprintf("name=%s", e.networkName)).CombinedOutput()
	if err != nil {
		e.logger.Log(string(out))
		e.logger.Log("Unable to check if docker network", e.networkName, "exists:", err.Error())
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

	tmpDir, err := ioutil.TempDir(dir, "e2e_")
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
	for _, c := range e.closers {
		c()
	}
	e.close()
	e.closed = true
}

func (e *DockerEnvironment) exec(cmd string, args ...string) *exec.Cmd {
	return e.execContext(context.Background(), cmd, args...)

}

func (e *DockerEnvironment) execContext(ctx context.Context, cmd string, args ...string) *exec.Cmd {
	c := NewCommand(cmd, args...)
	if e.verbose {
		e.logger.Log("dockerEnv:", c.toString())
	}
	return c.exec(ctx)
}

func (e *DockerEnvironment) close() {
	if e == nil || e.closed {
		return
	}

	// Kill the services in the opposite order.
	for i := len(e.started) - 1; i >= 0; i-- {
		n := e.started[i].Name()
		if err := e.started[i].Kill(); err != nil {
			e.logger.Log("Unable to kill service", n, ":", err.Error())
		}
	}

	// Ensure there are no leftover containers.
	if out, err := e.exec(
		"docker",
		"ps",
		"-a",
		"--quiet",
		"--filter",
		fmt.Sprintf("network=%s", e.networkName),
	).CombinedOutput(); err == nil {
		for _, containerID := range strings.Split(string(out), "\n") {
			containerID = strings.TrimSpace(containerID)
			if containerID == "" {
				continue
			}

			if out, err = e.exec("docker", "rm", "--force", containerID).CombinedOutput(); err != nil {
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
	if ok, err := e.existDockerNetwork(); ok || err != nil {
		if out, err := e.exec("docker", "network", "rm", e.networkName).CombinedOutput(); err != nil {
			e.logger.Log(string(out))
			e.logger.Log("Unable to remove docker network", e.networkName, ":", err.Error())
		}
	}

	if e.dir != "" {
		if err := e.exec("chmod", "-R", "777", e.dir).Run(); err != nil {
			e.logger.Log("Error while chmod sharedDir", e.dir, "err:", err)
		}
		if err := os.RemoveAll(e.dir); err != nil {
			e.logger.Log("Error while removing sharedDir", e.dir, "err:", err)
		}
	}
}
