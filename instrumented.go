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
	MetricPath       string // "/metrics" by default.
	Scheme           string // "http" by default.
}

// InstrumentedRunnable represents opinionated microservice with one port marked as HTTP port with metric endpoint.
type InstrumentedRunnable interface {
	Runnable

	MetricTargets() []MetricTarget
	Metrics() (string, error)
	WaitSumMetrics(expected MetricValueExpectation, metricNames ...string) error
	WaitSumMetricsWithOptions(expected MetricValueExpectation, metricNames []string, opts ...MetricsOption) error
	SumMetrics(metricNames []string, opts ...MetricsOption) ([]float64, error)
	WaitRemovedMetric(metricName string, opts ...MetricsOption) error
}

type FutureInstrumentedRunnable interface {
	Linkable

	// Init transforms future into runnable.
	Init(opts StartOptions) InstrumentedRunnable
}

// InstrumentedRunnableBuilder represents options that can be build into runnable and if
// you want Future or Initiated InstrumentedRunnableBuilder from it.
type InstrumentedRunnableBuilder interface {
	// WithPorts adds ports to runnable, allowing caller to
	// use `InternalEndpoint` and `Endpoint` methods by referencing port by name.
	WithPorts(ports map[string]int, instrumentedPortName string) InstrumentedRunnableBuilder
	// WithMetricPath allows adding customized metric path. "/metrics" by default.
	WithMetricPath(path string) InstrumentedRunnableBuilder
	// WithMetricScheme allows adding customized scheme. "http" or "https" values allowed. "http" by default.
	// If "https" is specified, insecure TLS will be performed.
	WithMetricScheme(scheme string) InstrumentedRunnableBuilder

	// Future returns future runnable
	Future() FutureInstrumentedRunnable
	// Init returns runnable.
	Init(opts StartOptions) InstrumentedRunnable
}

var _ InstrumentedRunnable = &instrumentedRunnable{}

type instrumentedRunnable struct {
	runnable
	Linkable
	builder RunnableBuilder

	name           string
	metricPortName string
	metricPath     string
	scheme         string

	waitBackoff *backoff.Backoff
}

func NewInstrumentedRunnable(env Environment, name string) InstrumentedRunnableBuilder {
	r := &instrumentedRunnable{name: name, scheme: "http", metricPath: "/metrics", builder: env.Runnable(name)}
	r.builder.WithConcreteType(r)
	return r
}

func (r *instrumentedRunnable) WithPorts(ports map[string]int, instrumentedPortName string) InstrumentedRunnableBuilder {
	if _, ok := ports[instrumentedPortName]; !ok {
		err := NewErrorer(r.name, errors.Errorf("metric port name %v does not exists in given ports", instrumentedPortName))
		r.Linkable = err
		r.runnable = err
		return r
	}
	r.builder.WithPorts(ports)
	r.metricPortName = instrumentedPortName
	return r
}

func (r *instrumentedRunnable) WithMetricPath(path string) InstrumentedRunnableBuilder {
	r.metricPath = path
	return r
}

func (r *instrumentedRunnable) WithMetricScheme(scheme string) InstrumentedRunnableBuilder {
	r.scheme = scheme
	return r
}

func (r *instrumentedRunnable) Future() FutureInstrumentedRunnable {
	if r.runnable != nil {
		// Error.
		return r
	}

	r.Linkable = r.builder.Future()
	return r
}

func (r *instrumentedRunnable) Init(opts StartOptions) InstrumentedRunnable {
	if r.runnable != nil {
		// Error.
		return r
	}

	inner := r.builder.Init(opts)
	r.Linkable = inner
	r.runnable = inner
	return r
}

func (r *instrumentedRunnable) MetricTargets() []MetricTarget {
	return []MetricTarget{{Scheme: r.scheme, MetricPath: r.metricPath, InternalEndpoint: r.InternalEndpoint(r.metricPortName)}}
}

func (r *instrumentedRunnable) Metrics() (_ string, err error) {
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
func (r *instrumentedRunnable) WaitSumMetrics(expected MetricValueExpectation, metricNames ...string) error {
	return r.WaitSumMetricsWithOptions(expected, metricNames)
}

func (r *instrumentedRunnable) WaitSumMetricsWithOptions(expected MetricValueExpectation, metricNames []string, opts ...MetricsOption) error {
	var (
		sums    []float64
		err     error
		options = buildMetricsOptions(opts)
	)

	metricsWaitBackoff := backoff.New(context.Background(), *options.waitBackoff)
	for metricsWaitBackoff.Reset(); metricsWaitBackoff.Ongoing(); {
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

		metricsWaitBackoff.Wait()
	}
	return errors.Errorf("unable to find metrics %s with expected values after %d retries. Last error: %v. Last values: %v", metricNames, metricsWaitBackoff.NumRetries(), err, sums)
}

// SumMetrics returns the sum of the values of each given metric names.
func (r *instrumentedRunnable) SumMetrics(metricNames []string, opts ...MetricsOption) ([]float64, error) {
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
func (r *instrumentedRunnable) WaitRemovedMetric(metricName string, opts ...MetricsOption) error {
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

func NewErrInstrumentedRunnable(name string, err error) InstrumentedRunnable {
	errr := NewErrorer(name, err)
	return &instrumentedRunnable{
		runnable: errr,
		Linkable: errr,
	}
}
