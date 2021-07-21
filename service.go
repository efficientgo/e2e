// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/efficientgo/tools/core/pkg/backoff"
	"github.com/efficientgo/tools/core/pkg/errcapture"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/common/expfmt"
)

var (
	errMissingMetric = errors.New("metric not found")
)

// Service represents microservice with optional ports which will be discoverable from docker
// with <name>:<port>. For connecting from test/hosts, use `Endpoint` method.
//
// Service can be reused (started and stopped many time), but it can represent only one running container
// at the time.
type Service struct {
	Started

	opts StartOptions
}

func NewService(
	name string,
	image string,
	command *Command,
	readiness ReadinessProbe,
	networkPorts map[string]int,
) *Service {
	return &Service{
		opts: StartOptions{
			Name:         name,
			Image:        image,
			NetworkPorts: networkPorts,
			Readiness:    readiness,
			Command:      command,
			WaitReadyBackoff: &backoff.Config{
				Min:        300 * time.Millisecond,
				Max:        600 * time.Millisecond,
				MaxRetries: 50, // Sometimes the CI is slow ¯\_(ツ)_/¯
			},
		},
	}
}

func (s *Service) Name() string { return s.opts.Name }

// Less often used options, only useful on start.

func (s *Service) SetBackoff(cfg backoff.Config) {
	s.opts.WaitReadyBackoff = &cfg
}

func (s *Service) SetEnvVars(env map[string]string) {
	s.opts.EnvVars = env
}

func (s *Service) SetUser(user string) {
	s.opts.User = user
}

func (s *Service) Start(_ log.Logger, env Environment) (_ StartedRunnable, err error) {
	r, err := env.Start(s.opts)
	if err != nil {
		return nil, err
	}
	s.Started = r
	return r, nil
}

// HTTPService represents opinionated microservice with one port marked as HTTP port with metric endpoint.
type HTTPService struct {
	*Service

	httpPort int
}

func NewHTTPService(
	name string,
	image string,
	command *Command,
	readiness ReadinessProbe,
	httpPort int,
	otherPorts ...int,
) *HTTPService {
	return &HTTPService{
		Service:  NewService(name, image, command, readiness, append(otherPorts, httpPort)...),
		httpPort: httpPort,
	}
}

func (s *HTTPService) Metrics() (_ string, err error) {
	// Map the container port to the local port.
	localPort := s.networkPortsContainerToLocal[s.httpPort]

	// Fetch metrics.
	res, err := (&http.Client{Timeout: 5 * time.Second}).Get(fmt.Sprintf("http://localhost:%d/metrics", localPort))
	if err != nil {
		return "", err
	}

	// Check the status code.
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("unexpected status code %d while fetching metrics", res.StatusCode)
	}

	defer errcapture.ExhaustClose(&err, res.Body, "metrics response")
	body, err := ioutil.ReadAll(res.Body)

	return string(body), err
}

func (s *HTTPService) HTTPPort() int {
	return s.httpPort
}

func (s *HTTPService) HTTPEndpoint() string {
	return s.Endpoint(s.httpPort)
}

func (s *HTTPService) NetworkHTTPEndpoint() string {
	return s.NetworkEndpoint(s.httpPort)
}

func (s *HTTPService) NetworkHTTPEndpointFor(networkName string) string {
	return s.NetworkEndpointFor(networkName, s.httpPort)
}

// WaitSumMetrics waits for at least one instance of each given metric names to be present and their sums, returning true
// when passed to given isExpected(...).
func (s *HTTPService) WaitSumMetrics(isExpected func(sums ...float64) bool, metricNames ...string) error {
	return s.WaitSumMetricsWithOptions(isExpected, metricNames)
}

func (s *HTTPService) WaitSumMetricsWithOptions(isExpected func(sums ...float64) bool, metricNames []string, opts ...MetricsOption) error {
	var (
		sums    []float64
		err     error
		options = buildMetricsOptions(opts)
	)

	for s.backoff.Reset(); s.backoff.Ongoing(); {
		sums, err = s.SumMetrics(metricNames, opts...)
		if options.waitMissingMetrics && errors.Is(err, errMissingMetric) {
			continue
		}
		if err != nil {
			return err
		}

		if isExpected(sums...) {
			return nil
		}

		s.backoff.Wait()
	}

	return fmt.Errorf("unable to find metrics %s with expected values. Last error: %v. Last values: %v", metricNames, err, sums)
}

// SumMetrics returns the sum of the values of each given metric names.
func (s *HTTPService) SumMetrics(metricNames []string, opts ...MetricsOption) ([]float64, error) {
	options := buildMetricsOptions(opts)
	sums := make([]float64, len(metricNames))

	metrics, err := s.Metrics()
	if err != nil {
		return nil, err
	}

	var tp expfmt.TextParser
	families, err := tp.TextToMetricFamilies(strings.NewReader(metrics))
	if err != nil {
		return nil, err
	}

	for i, m := range metricNames {
		sums[i] = 0.0

		// Get the metric family.
		mf, ok := families[m]
		if !ok {
			if options.skipMissingMetrics {
				continue
			}

			return nil, errors.Wrapf(errMissingMetric, "metric=%s service=%s", m, s.name)
		}

		// Filter metrics.
		metrics := filterMetrics(mf.GetMetric(), options)
		if len(metrics) == 0 {
			if options.skipMissingMetrics {
				continue
			}

			return nil, errors.Wrapf(errMissingMetric, "metric=%s service=%s", m, s.name)
		}

		sums[i] = SumValues(getValues(metrics, options))
	}

	return sums, nil
}

// WaitRemovedMetric waits until a metric disappear from the list of metrics exported by the service.
func (s *HTTPService) WaitRemovedMetric(metricName string, opts ...MetricsOption) error {
	options := buildMetricsOptions(opts)

	for s.backoff.Reset(); s.backoff.Ongoing(); {
		// Fetch metrics.
		metrics, err := s.Metrics()
		if err != nil {
			return err
		}

		// Parse metrics.
		var tp expfmt.TextParser
		families, err := tp.TextToMetricFamilies(strings.NewReader(metrics))
		if err != nil {
			return err
		}

		// Get the metric family.
		mf, ok := families[metricName]
		if !ok {
			return nil
		}

		// Filter metrics.
		if len(filterMetrics(mf.GetMetric(), options)) == 0 {
			return nil
		}

		s.backoff.Wait()
	}

	return fmt.Errorf("the metric %s is still exported by %s", metricName, s.name)
}
