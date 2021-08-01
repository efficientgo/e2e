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

var errMissingMetric = errors.New("metric not found")

type MetricTarget struct {
	InternalEndpoint string
	MetricPath       string
}

// Instrumented is implemented by all instrumented runnables.
type Instrumented interface {
	MetricTargets() []MetricTarget
}

var _ Instrumented = &InstrumentedRunnable{}

// InstrumentedRunnable represents opinionated microservice with one port marked as HTTP port with metric endpoint.
type InstrumentedRunnable struct {
	Runnable

	name           string
	metricPortName string
	metricPath     string
	ports          map[string]int

	waitBackoff *backoff.Backoff
}

type FutureInstrumentedRunnable struct {
	FutureRunnable

	r *InstrumentedRunnable
}

func NewInstrumentedRunnable(
	env Environment,
	name string,
	ports map[string]int,
	metricPortName string,
) *FutureInstrumentedRunnable {
	f := &FutureInstrumentedRunnable{
		r: &InstrumentedRunnable{name: name, ports: ports, metricPortName: metricPortName, metricPath: "/metrics"},
	}

	if _, ok := ports[metricPortName]; !ok {
		f.FutureRunnable = NewErrorer(name, errors.Errorf("metric port name %v does not exists in given ports", metricPortName))
		return f
	}
	f.FutureRunnable = env.Runnable(name).WithPorts(ports).WithConcreteType(f.r).Future()
	return f
}

func NewErrInstrumentedRunnable(name string, err error) *InstrumentedRunnable {
	return &InstrumentedRunnable{
		Runnable: NewErrorer(name, err),
	}
}

func (r *FutureInstrumentedRunnable) Init(opts StartOptions) *InstrumentedRunnable {
	if opts.WaitReadyBackoff == nil {
		opts.WaitReadyBackoff = &backoff.Config{
			Min:        300 * time.Millisecond,
			Max:        600 * time.Millisecond,
			MaxRetries: 50, // Sometimes the CI is slow ¯\_(ツ)_/¯.
		}
	}

	r.r.waitBackoff = backoff.New(context.Background(), *opts.WaitReadyBackoff)
	r.r.Runnable = r.FutureRunnable.Init(opts)
	return r.r
}

func (r *InstrumentedRunnable) MetricTargets() []MetricTarget {
	return []MetricTarget{{MetricPath: r.metricPath, InternalEndpoint: r.InternalEndpoint(r.metricPortName)}}
}

func (r *InstrumentedRunnable) Metrics() (_ string, err error) {
	if !r.IsRunning() {
		return "", errors.Errorf("%s is not running", r.Name())
	}

	// Fetch metrics.
	res, err := (&http.Client{Timeout: 5 * time.Second}).Get(fmt.Sprintf("http://%s/metrics", r.Endpoint(r.metricPortName)))
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
// returning true when passed to given expected(...).
func (r *InstrumentedRunnable) WaitSumMetrics(expected MetricValueExpectation, metricNames ...string) error {
	return r.WaitSumMetricsWithOptions(expected, metricNames)
}

func (r *InstrumentedRunnable) WaitSumMetricsWithOptions(expected MetricValueExpectation, metricNames []string, opts ...MetricsOption) error {
	var (
		sums    []float64
		err     error
		options = buildMetricsOptions(opts)
	)

	for r.waitBackoff.Reset(); r.waitBackoff.Ongoing(); {
		sums, err = r.SumMetrics(metricNames, opts...)
		if options.waitMissingMetrics && errors.Is(err, errMissingMetric) {
			continue
		}
		if err != nil {
			return err
		}

		if expected(sums...) {
			return nil
		}

		r.waitBackoff.Wait()
	}
	return errors.Errorf("unable to find metrics %s with expected values after %d retries. Last error: %v. Last values: %v", metricNames, r.waitBackoff.NumRetries(), err, sums)
}

// SumMetrics returns the sum of the values of each given metric names.
func (r *InstrumentedRunnable) SumMetrics(metricNames []string, opts ...MetricsOption) ([]float64, error) {
	options := buildMetricsOptions(opts)
	sums := make([]float64, len(metricNames))

	metrics, err := r.Metrics()
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

			return nil, errors.Wrapf(errMissingMetric, "metric=%s service=%s", m, r.Name())
		}

		// Filter metrics.
		metrics := filterMetrics(mf.GetMetric(), options)
		if len(metrics) == 0 {
			if options.skipMissingMetrics {
				continue
			}

			return nil, errors.Wrapf(errMissingMetric, "metric=%s service=%s", m, r.Name())
		}

		sums[i] = SumValues(getValues(metrics, options))
	}

	return sums, nil
}

// WaitRemovedMetric waits until a metric disappear from the list of metrics exported by the service.
func (r *InstrumentedRunnable) WaitRemovedMetric(metricName string, opts ...MetricsOption) error {
	options := buildMetricsOptions(opts)

	for r.waitBackoff.Reset(); r.waitBackoff.Ongoing(); {
		// Fetch metrics.
		metrics, err := r.Metrics()
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

		r.waitBackoff.Wait()
	}

	return errors.Errorf("the metric %s is still exported by %s", metricName, r.Name())
}
