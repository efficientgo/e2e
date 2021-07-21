// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e

import (
	"crypto/rand"
	"fmt"
	"os"

	"github.com/pkg/errors"
)

// Runnable is the smallest entity that Scenario can run, stop and manage.
// It represents single runnable, able to run using Environment.
type Runnable interface {
	// Name returns unique name for the Runnable instance.
	Name() string

	// Start tells Runnable to start it's execution in given environment.
	Start(logger Logger, env Environment) (StartedRunnable, error)
}

// Scenario allows to manage executables for single run scenario.
type Scenario struct {
	opts scenarioOptions
	env  Environment

	runnablesIndex map[string]int
	runnables      []StartedRunnable
}

// ScenarioOption defined the signature of a function used to manipulate options.
type ScenarioOption func(*scenarioOptions)

type scenarioOptions struct {
	envName        string
	envConstructor EnvironmentConstructor
	logger         Logger
}

// WithEnvironmentName tells scenario to use custom environment name instead of UUID.
// Prefer reusing names so no hanging environments are registered.
func WithEnvironmentName(envName string) ScenarioOption {
	return func(o *scenarioOptions) {
		o.envName = envName
	}
}

// WithEnvironmentConstructor tells scenario to use custom environment constructor instead of the default NewDockerEnv.
func WithEnvironmentConstructor(c EnvironmentConstructor) ScenarioOption {
	return func(o *scenarioOptions) {
		o.envConstructor = c
	}
}

// WithLogger tells scenario to use custom logger to default one (stdout).
func WithLogger(logger Logger) ScenarioOption {
	return func(o *scenarioOptions) {
		o.logger = logger
	}
}

// NewScenario creates new Scenario.
func NewScenario(opts ...ScenarioOption) (_ *Scenario, err error) {
	s := &Scenario{}
	for _, o := range opts {
		o(&s.opts)
	}
	if s.opts.envName == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		s.opts.envName = fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	}
	if s.opts.envConstructor == nil {
		s.opts.envConstructor = NewDockerEnv
	}
	if s.opts.logger == nil {
		s.opts.logger = NewLogger(os.Stdout)
	}

	s.env, err = s.opts.envConstructor(s.opts.logger, s.opts.envName)
	if err != nil {
		return nil, errors.Wrap(err, "constructing environment")
	}
	return s, nil
}

func (s *Scenario) isRegistered(name string) bool {
	_, ok := s.runnablesIndex[name]
	return ok
}

func (s *Scenario) StartAndWaitReady(runnables ...Runnable) error {
	if err := s.Start(runnables...); err != nil {
		return err
	}
	return s.WaitReady(runnables...)
}

func (s *Scenario) Start(runnables ...Runnable) error {
	for _, r := range runnables {
		s.opts.logger.Log("Starting", r.Name())

		// Ensure another service with the same name doesn't exist.
		if s.isRegistered(r.Name()) {
			return errors.Errorf("another service with the same name '%s' has already been started", r.Name())
		}

		// Start the service.
		started, err := r.Start(s.opts.logger, s.env)
		if err != nil {
			return err
		}

		// Add to the list of services.
		s.runnablesIndex[r.Name()] = len(s.runnables)
		s.runnables = append(s.runnables, started)
	}

	return nil
}

func (s *Scenario) getStartedRunnable(r Runnable) StartedRunnable {
	return s.runnables[s.runnablesIndex[r.Name()]]
}
func (s *Scenario) Stop(runnables ...Runnable) error {
	for _, r := range runnables {
		if !s.isRegistered(r.Name()) {
			return errors.Errorf("unable to stop service %s because it does not exist", r.Name())
		}

		if err := s.getStartedRunnable(r).stop(); err != nil {
			return err
		}

		// Remove the service from the list of services.
		s.runnables = append(s.runnables[:s.runnablesIndex[r.Name()]], s.runnables[s.runnablesIndex[r.Name()]+1:]...)
		delete(s.runnablesIndex, r.Name())
	}
	return nil
}

func (s *Scenario) WaitReady(runnables ...Runnable) error {
	for _, r := range runnables {
		if !s.isRegistered(r.Name()) {
			return errors.Errorf("unable to wait for service %s because it does not exist", r.Name())
		}
		if err := s.getStartedRunnable(r).waitReady(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scenario) Close() {
	if s == nil {
		return
	}
	// Kill the services in the opposite order.
	for i := len(s.runnables) - 1; i >= 0; i-- {
		if err := s.runnables[i].kill(); err != nil {
			rname := ""
			// Find name.
			for name, ind := range s.runnablesIndex {
				if i == ind {
					rname = name
					break
				}
			}
			s.opts.logger.Log("Unable to kill service", rname, ":", err.Error())
		}
	}
	s.env.Close()
}
