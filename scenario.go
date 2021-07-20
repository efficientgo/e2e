// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e

import (
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const ContainerSharedDir = "/shared"

// Service is unified service interface that Scenario can manage.
type Service interface {
	Name() string
	Start(logger log.Logger, networkName, dir string) error
	WaitReady() error

	// It should be ok to Stop and Kill more than once, with next invokes being noop.
	Kill() error
	Stop() error
}

type logger struct {
	w io.Writer
}

func NewLogger(w io.Writer) *logger {
	return &logger{
		w: w,
	}
}

func (l *logger) Log(keyvals ...interface{}) error {
	b := strings.Builder{}
	b.WriteString(time.Now().Format("15:04:05"))

	for _, v := range keyvals {
		b.WriteString(" " + fmt.Sprintf("%v", v))
	}

	b.WriteString("\n")

	_, err := l.w.Write([]byte(b.String()))
	return err
}

// Scenario allows to manage deployments for single testing scenario.
type Scenario struct {
	o         scenarioOptions
	sharedDir string

	services []Service
}

// ScenarioOption defined the signature of a function used to manipulate options.
type ScenarioOption func(*scenarioOptions)

type scenarioOptions struct {
	networkName string
	logger      log.Logger
}

// WithNetworkName tells scenario to use custom network name instead of UUID.
func WithNetworkName(networkName string) ScenarioOption {
	return func(o *scenarioOptions) {
		o.networkName = networkName
	}
}

// WithLogger tells scenario to use custom logger default one (stdout).
func WithLogger(logger log.Logger) ScenarioOption {
	return func(o *scenarioOptions) {
		o.logger = logger
	}
}

// NewScenario creates new Scenario.
func NewScenario(opts ...ScenarioOption) (_ *Scenario, err error) {
	s := &Scenario{}
	for _, o := range opts {
		o(&s.o)
	}
	if s.o.networkName == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		s.o.networkName = fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	}
	if s.o.logger == nil {
		s.o.logger = NewLogger(os.Stdout)
	}

	s.sharedDir, err = getTempDirectory()
	if err != nil {
		return nil, err
	}

	// Force a shutdown in order to cleanup from a spurious situation in case
	// the previous tests run didn't cleanup correctly.
	s.shutdown()

	// Setup the docker network.
	if out, err := RunCommandAndGetOutput("docker", "network", "create", s.o.networkName); err != nil {
		s.o.logger.Log(string(out))
		s.clean()
		return nil, errors.Wrapf(err, "create docker network '%s'", s.o.networkName)
	}

	return s, nil
}

// SharedDir returns the absolute path of the directory on the host that is shared with all services in docker.
func (s *Scenario) SharedDir() string {
	return s.sharedDir
}

// NetworkName returns the network name that scenario is responsible for.
func (s *Scenario) NetworkName() string {
	return s.o.networkName
}

func (s *Scenario) isRegistered(name string) bool {
	for _, service := range s.services {
		if service.Name() == name {
			return true
		}
	}
	return false
}

func (s *Scenario) StartAndWaitReady(services ...Service) error {
	if err := s.Start(services...); err != nil {
		return err
	}
	return s.WaitReady(services...)
}

func (s *Scenario) Start(services ...Service) error {
	for _, service := range services {
		s.o.logger.Log("Starting", service.Name())

		// Ensure another service with the same name doesn't exist.
		if s.isRegistered(service.Name()) {
			return fmt.Errorf("another service with the same name '%s' has already been started", service.Name())
		}

		// Start the service.
		if err := service.Start(s.o.logger, s.o.networkName, s.SharedDir()); err != nil {
			return err
		}

		// Add to the list of services.
		s.services = append(s.services, service)
	}

	return nil
}

func (s *Scenario) Stop(services ...Service) error {
	for _, service := range services {
		if !s.isRegistered(service.Name()) {
			return fmt.Errorf("unable to stop service %s because it does not exist", service.Name())
		}
		if err := service.Stop(); err != nil {
			return err
		}

		// Remove the service from the list of services.
		for i, entry := range s.services {
			if entry.Name() == service.Name() {
				s.services = append(s.services[:i], s.services[i+1:]...)
				break
			}
		}
	}
	return nil
}

func (s *Scenario) WaitReady(services ...Service) error {
	for _, service := range services {
		if !s.isRegistered(service.Name()) {
			return fmt.Errorf("unable to wait for service %s because it does not exist", service.Name())
		}
		if err := service.WaitReady(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scenario) Close() {
	if s == nil {
		return
	}
	s.shutdown()
	s.clean()
}

// TODO(bwplotka): Add comments.
func (s *Scenario) clean() {
	if err := os.RemoveAll(s.sharedDir); err != nil {
		s.o.logger.Log("error while removing sharedDir", s.sharedDir, "err:", err)
	}
}

func (s *Scenario) shutdown() {
	// Kill the services in the opposite order.
	for i := len(s.services) - 1; i >= 0; i-- {
		if err := s.services[i].Kill(); err != nil {
			s.o.logger.Log("Unable to kill service", s.services[i].Name(), ":", err.Error())
		}
	}

	// Ensure there are no leftover containers.
	if out, err := RunCommandAndGetOutput(
		"docker",
		"ps",
		"-a",
		"--quiet",
		"--filter",
		fmt.Sprintf("network=%s", s.o.networkName),
	); err == nil {
		for _, containerID := range strings.Split(string(out), "\n") {
			containerID = strings.TrimSpace(containerID)
			if containerID == "" {
				continue
			}

			if out, err = RunCommandAndGetOutput("docker", "rm", "--force", containerID); err != nil {
				s.o.logger.Log(string(out))
				s.o.logger.Log("Unable to cleanup leftover container", containerID, ":", err.Error())
			}
		}
	} else {
		s.o.logger.Log(string(out))
		s.o.logger.Log("Unable to cleanup leftover containers:", err.Error())
	}

	// Teardown the docker network. In case the network does not exists (ie. this function
	// is called during the setup of the scenario) we skip the removal in order to not log
	// an error which may be misleading.
	if ok, err := existDockerNetwork(s.o.logger, s.o.networkName); ok || err != nil {
		if out, err := RunCommandAndGetOutput("docker", "network", "rm", s.o.networkName); err != nil {
			s.o.logger.Log(string(out))
			s.o.logger.Log("Unable to remove docker network", s.o.networkName, ":", err.Error())
		}
	}
}

func existDockerNetwork(logger log.Logger, networkName string) (bool, error) {
	out, err := RunCommandAndGetOutput("docker", "network", "ls", "--quiet", "--filter", fmt.Sprintf("name=%s", networkName))
	if err != nil {
		logger.Log(string(out))
		logger.Log("Unable to check if docker network", networkName, "exists:", err.Error())
		return false, err
	}

	return strings.TrimSpace(string(out)) != "", nil
}

// getTempDirectory creates a temporary directory for shared integration
// test files, either in the working directory or a directory referenced by
// the E2E_TEMP_DIR environment variable.
func getTempDirectory() (string, error) {
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

	tmpDir, err := ioutil.TempDir(dir, "e2e_integration_test")
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
