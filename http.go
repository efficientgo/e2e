// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/efficientgo/tools/core/pkg/backoff"
	"github.com/efficientgo/tools/core/pkg/errcapture"
	"github.com/pkg/errors"
	"github.com/prometheus/common/expfmt"
)

var (
	errMissingMetric = errors.New("metric not found")
)

// HTTPService represents opinionated microservice with one port marked as HTTP port with metric endpoint.
type HTTPService struct {
	Runnable
	httpPortName string
	waitBackoff  *backoff.Backoff
}

func NewHTTPService(
	env Environment,
	opts StartOptions,
	httpPortName string,
) *HTTPService {
	if opts.WaitReadyBackoff == nil {
		opts.WaitReadyBackoff = &backoff.Config{
			Min:        300 * time.Millisecond,
			Max:        600 * time.Millisecond,
			MaxRetries: 50, // Sometimes the CI is slow ¯\_(ツ)_/¯.
		}
	}

	h := &HTTPService{httpPortName: httpPortName, waitBackoff: backoff.New(context.Background(), *opts.WaitReadyBackoff)}
	if _, ok := opts.NetworkPorts[httpPortName]; !ok {
		h.Runnable = NewErrRunnable(opts.Name, errors.Errorf("http port name %v does not exists in NetworkPorts", httpPortName))
		return h
	}
	h.Runnable = env.Runnable(opts)
	return h
}

func (s *HTTPService) Metrics() (_ string, err error) {
	// Fetch metrics.
	res, err := (&http.Client{Timeout: 5 * time.Second}).Get(fmt.Sprintf("http://%s/metrics", s.Endpoint(s.httpPortName)))
	if err != nil {
		return "", err
	}

	// Check the status code.
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", errors.Errorf("unexpected status code %d while fetching metrics", res.StatusCode)
	}
	defer errcapture.ExhaustClose(&err, res.Body, "metrics response")

	body, err := ioutil.ReadAll(res.Body)
	return string(body), err
}

// WaitSumMetrics waits for at least one instance of each given metric names to be present and their sums,
// returning true when passed to given isExpected(...).
func (s *HTTPService) WaitSumMetrics(isExpected func(sums ...float64) bool, metricNames ...string) error {
	return s.WaitSumMetricsWithOptions(isExpected, metricNames)
}

func (s *HTTPService) WaitSumMetricsWithOptions(isExpected func(sums ...float64) bool, metricNames []string, opts ...MetricsOption) error {
	var (
		sums    []float64
		err     error
		options = buildMetricsOptions(opts)
	)

	for s.waitBackoff.Reset(); s.waitBackoff.Ongoing(); {
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

		s.waitBackoff.Wait()
	}

	return errors.Errorf("unable to find metrics %s with expected values. Last error: %v. Last values: %v", metricNames, err, sums)
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

			return nil, errors.Wrapf(errMissingMetric, "metric=%s service=%s", m, s.Name())
		}

		// Filter metrics.
		metrics := filterMetrics(mf.GetMetric(), options)
		if len(metrics) == 0 {
			if options.skipMissingMetrics {
				continue
			}

			return nil, errors.Wrapf(errMissingMetric, "metric=%s service=%s", m, s.Name())
		}

		sums[i] = SumValues(getValues(metrics, options))
	}

	return sums, nil
}

// WaitRemovedMetric waits until a metric disappear from the list of metrics exported by the service.
func (s *HTTPService) WaitRemovedMetric(metricName string, opts ...MetricsOption) error {
	options := buildMetricsOptions(opts)

	for s.waitBackoff.Reset(); s.waitBackoff.Ongoing(); {
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

		s.waitBackoff.Wait()
	}

	return errors.Errorf("the metric %s is still exported by %s", metricName, s.Name())
}
