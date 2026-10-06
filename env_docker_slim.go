// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/efficientgo/core/backoff"
	"github.com/efficientgo/core/errors"
)

type DockerSlimOptions struct {
	Name           string
	ExtraBuildArgs []string
}

func (e *DockerEnvironment) RunnableSlim(opts DockerSlimOptions) RunnableBuilder {
	if e.closed {
		return errorer{name: opts.Name, err: errors.New("environment close was invoked already.")}
	}

	if e.isRegistered(opts.Name) {
		return errorer{name: opts.Name, err: errors.Newf("there is already one runnable created with the same name %v", opts.Name)}
	}

	d := &dockerSlimRunnable{
		container: &dockerRunnable{
			env:        e,
			name:       opts.Name,
			logger:     e.logger,
			ports:      map[string]int{},
			hostPorts:  map[string]int{},
			extensions: map[any]any{},
		},
		containerName:  dockerNetworkContainerHost(e.networkName, opts.Name),
		extraBuildArgs: opts.ExtraBuildArgs,
	}
	if err := os.MkdirAll(d.Dir(), 0750); err != nil {
		return errorer{name: opts.Name, err: err}
	}
	e.register(opts.Name)
	return d
}

func (e *DockerEnvironment) buildDockerSlimRunArgs(name string, ports map[string]int, extraBuildArgs []string, opts StartOptions) []string {
	args := []string{"--show-clogs", "--show-blogs", "--label", "application=" + dockerNetworkContainerHost(e.networkName, name),
		"--http-probe=false", "--target", opts.Image, "--tag", fmt.Sprintf("%s-slim", opts.Image),
		"--network=" + e.networkName, "--hostname=" + name, "--continue-after", "signal"}

	if len(extraBuildArgs) > 0 {
		args = append(args, extraBuildArgs...)
	}

	// Mount the shared/ directory into the container. We share all containers dir to each other to allow easier scenarios.
	args = append(args, "--mount", fmt.Sprintf("%s:%s:z", e.dir, e.dir))

	for _, v := range opts.Volumes {
		args = append(args, "--mount", v)
	}

	// Environment variables
	for name, value := range opts.EnvVars {
		args = append(args, "--env", name+"="+value)
	}

	if opts.User != "" {
		args = append(args, "--user", opts.User)
	}

	// Published ports.
	for _, port := range ports {
		args = append(args, "--expose", strconv.Itoa(port), "--publish-port", strconv.Itoa(port))
	}

	// Disable entrypoint if required.
	if opts.Command.EntrypointDisabled {
		args = append(args, "--entrypoint", "")
	}

	var cmdArgs string

	if opts.Command.Cmd != "" {
		cmdArgs += opts.Command.Cmd
	}
	if len(opts.Command.Args) > 0 {
		for _, arg := range opts.Command.Args {
			if cmdArgs != "" {
				cmdArgs += fmt.Sprintf(" %s", arg)
			} else {
				cmdArgs += arg
			}
		}
	}

	if cmdArgs != "" {
		args = append(args, "--cmd", cmdArgs)
	}
	return args
}

type dockerSlimRunnable struct {
	container      *dockerRunnable
	containerName  string
	extraBuildArgs []string
	pid            int
}

func (d *dockerSlimRunnable) Name() string {
	return d.containerName
}

func (d *dockerSlimRunnable) BuildErr() error {
	return nil
}

func (d *dockerSlimRunnable) Dir() string {
	return filepath.Join(d.container.env.dir, "data", d.Name())
}

func (d *dockerSlimRunnable) InternalDir() string {
	return d.Dir()
}

func (d *dockerSlimRunnable) Init(opts StartOptions) Runnable {
	if opts.WaitReadyBackoff == nil {
		opts.WaitReadyBackoff = &backoff.Config{
			Min:        300 * time.Millisecond,
			Max:        600 * time.Millisecond,
			MaxRetries: 50, // Sometimes the CI is slow ¯\_(ツ)_/¯.
		}
	}

	d.container.opts = opts
	d.container.waitBackoffReady = backoff.New(context.Background(), *opts.WaitReadyBackoff)
	return d
}

func (d *dockerSlimRunnable) WithPorts(ports map[string]int) RunnableBuilder {
	d.container.ports = ports
	return d
}

func (d *dockerSlimRunnable) SetMetadata(key, value any) {
	d.container.extensions[key] = value
}

func (d *dockerSlimRunnable) GetMetadata(key any) (any, bool) {
	v, ok := d.container.extensions[key]
	return v, ok
}

func (d *dockerSlimRunnable) Future() FutureRunnable {
	return d
}

func (d *dockerSlimRunnable) IsRunning() bool {
	return d.container.usedNetworkName != ""
}

// Start starts runnable.
func (d *dockerSlimRunnable) Start() (err error) {
	if d.IsRunning() {
		return errors.Newf("%v is running. Stop or kill it first to restart.", d.Name())
	}

	d.container.logger.Log("Starting", d.Name())

	// In case of any error, if the container was already created, we
	// have to cleanup removing it. We ignore the error of the "docker rm"
	// because we don't know if the container was created or not.
	defer func() {
		if err != nil {
			_, _ = d.container.env.exec("docker", "rm", "--force", d.container.name).CombinedOutput()
		}
	}()

	// Make sure the image is available locally; if not wait for it to download.
	if err := d.container.prePullImage(context.TODO()); err != nil {
		return err
	}

	cmd := d.container.env.exec("docker-slim", append([]string{"build"}, d.container.env.buildDockerSlimRunArgs(d.container.name, d.container.ports,
		d.extraBuildArgs, d.container.opts)...)...)
	l := &LinePrefixLogger{prefix: d.Name() + ": ", logger: d.container.logger}
	cmd.Stdout = l
	cmd.Stderr = l
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return err
	}
	d.pid = cmd.Process.Pid
	d.container.usedNetworkName = d.container.env.networkName

	if err := d.addName(); err != nil {
		return err
	}

	// Wait until the container has been started.
	if err := d.waitForRunning(); err != nil {
		return err
	}

	if err := d.container.env.registerStarted(d); err != nil {
		return err
	}

	// Get the dynamic local ports mapped to the container.
	for portName, containerPort := range d.container.ports {
		d.container.hostPorts[portName] = containerPort
	}

	d.container.logger.Log("Ports for container", d.container.containerName(), ">> Local ports:", d.container.ports, "Ports available from host:", d.container.hostPorts)
	return nil
}

func (d *dockerSlimRunnable) Stop() error {
	if d.pid == 0 {
		return nil
	}

	if err := syscall.Kill(d.pid, syscall.SIGUSR1); err != nil {
		return err
	}

	process, err := os.FindProcess(d.pid)

	if err != nil {
		return err
	}

	if _, err := process.Wait(); err != nil {
		return err
	}

	d.container.usedNetworkName = ""
	return d.container.env.registerStopped(d.Name())
}

func (d *dockerSlimRunnable) Kill() error {
	if d.pid == 0 {
		return nil
	}

	if err := syscall.Kill(d.pid, syscall.SIGUSR1); err != nil {
		return d.container.Kill()
	}

	process, err := os.FindProcess(d.pid)

	if err != nil {
		return d.container.Kill()
	}

	if _, err := process.Wait(); err != nil {
		return d.container.Kill()
	}

	d.container.usedNetworkName = ""
	return d.container.Kill()
}

// Endpoint returns external (from host perspective) service endpoint (host:port) for given port name.
// External means that it will be accessible only from host, but not from docker containers.
//
// If your service is not running, this method returns incorrect `stopped` endpoint.
func (d *dockerSlimRunnable) Endpoint(portName string) string {
	return d.container.Endpoint(portName)
}

// InternalEndpoint returns internal service endpoint (host:port) for given internal port.
// Internal means that it will be accessible only from docker containers within the network that this
// service is running in. If you configure your local resolver with docker DNS namespace you can access it from host
// as well. Use `Endpoint` for host access.
func (d *dockerSlimRunnable) InternalEndpoint(portName string) string {
	return d.container.InternalEndpoint(portName)
}

func (d *dockerSlimRunnable) Ready() error {
	return d.container.Ready()
}

func (d *dockerSlimRunnable) WaitReady() error {
	err := d.container.WaitReady()
	return err
}

// Exec runs the provided command against the docker container specified by this
// service.
func (d *dockerSlimRunnable) Exec(command Command, opts ...ExecOption) error {
	if !d.IsRunning() {
		return errors.Newf("service %s is stopped", d.Name())
	}

	l := &LinePrefixLogger{prefix: d.Name() + "-exec: ", logger: d.container.logger}
	o := ExecOptions{Stdout: l, Stderr: l}
	for _, opt := range opts {
		opt(&o)
	}

	args := []string{"exec", d.container.name}
	args = append(args, command.Cmd)
	args = append(args, command.Args...)
	cmd := d.container.env.exec("docker", args...)
	cmd.Stdout = o.Stdout
	cmd.Stderr = o.Stderr
	return cmd.Run()
}

func (d *dockerSlimRunnable) addName() error {
	var out []byte
	var err error
	for d.container.waitBackoffReady.Reset(); d.container.waitBackoffReady.Ongoing(); {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err = d.container.env.execContext(
			ctx,
			"docker",
			"ps",
			"-a",
			"-f",
			"label=application="+d.containerName,
			"--format",
			"{{.Names}}",
		).CombinedOutput()

		if err != nil {
			d.container.waitBackoffReady.Wait()
			continue
		}
		if out == nil {
			err = errors.Newf("nil output")
			d.container.waitBackoffReady.Wait()
			continue
		}

		name := strings.TrimSpace(string(out))
		if strings.Contains(name, "\n") || !strings.HasPrefix(name, "dockerslimk_") {
			err = errors.Newf("unexpected output: %q", name)
			d.container.waitBackoffReady.Wait()
			continue
		}

		d.container.name = name
		return nil
	}

	if len(out) > 0 {
		d.container.logger.Log(string(out))
	}
	return errors.Wrapf(err, "docker container %s failed to start", d.Name())
}

func (d *dockerSlimRunnable) waitForRunning() (err error) {
	if !d.IsRunning() {
		return errors.Newf("service %s is stopped", d.Name())
	}

	var out []byte
	for d.container.waitBackoffReady.Reset(); d.container.waitBackoffReady.Ongoing(); {
		// Enforce a timeout on the command execution because we've seen some flaky tests
		// stuck here.

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err = d.container.env.execContext(
			ctx,
			"docker",
			"inspect",
			"--format={{json .State.Running}}",
			d.container.name,
		).CombinedOutput()

		if err != nil {
			d.container.waitBackoffReady.Wait()
			continue
		}

		if out == nil {
			err = errors.Newf("nil output")
			d.container.waitBackoffReady.Wait()
			continue
		}

		str := strings.TrimSpace(string(out))
		if str != "true" {
			err = errors.Newf("unexpected output: %q", str)
			d.container.waitBackoffReady.Wait()
			continue
		}

		return nil
	}

	if len(out) > 0 {
		d.container.logger.Log(string(out))
	}
	return errors.Wrapf(err, "docker container %s failed to start", d.Name())
}
