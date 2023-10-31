// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2emon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/efficientgo/core/backoff"
	"github.com/efficientgo/core/errcapture"
	"github.com/efficientgo/core/errors"
	"github.com/efficientgo/e2e"
	"github.com/prometheus/common/expfmt"
)

var errMissingMetric = errors.New("metric not found")

// Target represents scrape target for Prometheus to use.
type Target struct {
	InternalEndpoint string
	MetricPath       string // "/metrics" by default.
	Scheme           string // "http" by default.
}

// Instrumented represents methods for instrumented runnable focused on accessing instrumented metrics.
type Instrumented interface {
	MetricTargets() []Target
	Metrics() (string, error)
	WaitSumMetrics(expected MetricValueExpectation, metricNames ...string) error
	WaitSumMetricsWithOptions(expected MetricValueExpectation, metricNames []string, opts ...MetricsOption) error
	SumMetrics(metricNames []string, opts ...MetricsOption) ([]float64, error)
	WaitRemovedMetric(metricName string, opts ...MetricsOption) error
}

var _ Instrumented = &InstrumentedRunnable{}

// InstrumentedRunnable represents runnable with instrumented Prometheus metric endpoint on a certain port.
type InstrumentedRunnable struct {
	e2e.Runnable

	metricPortName string
	metricPath     string
	scheme         string

	waitBackoff *backoff.Backoff
}

type rOpt struct {
	metricPath  string
	scheme      string
	waitBackoff *backoff.Backoff
}

// WithInstrumentedMetricPath sets a custom path for metrics page. "/metrics" by default.
func WithInstrumentedMetricPath(metricPath string) InstrumentedOption {
	return func(o *rOpt) {
		o.metricPath = metricPath
	}
}

// WithInstrumentedScheme allows adding customized scheme. "http" or "https" values allowed. "http" by default.
// If "https" is specified, insecure TLS will be performed.
func WithInstrumentedScheme(scheme string) InstrumentedOption {
	return func(o *rOpt) {
		o.scheme = scheme
	}
}

// WithInstrumentedWaitBackoff allows customizing wait backoff when accessing or asserting on the metric endpoint.
func WithInstrumentedWaitBackoff(waitBackoff *backoff.Backoff) InstrumentedOption {
	return func(o *rOpt) {
		o.waitBackoff = waitBackoff
	}
}

// InstrumentedOption is a variadic option for AsInstrumented.
type InstrumentedOption func(*rOpt)

// AsInstrumented wraps e2e.Runnable with InstrumentedRunnable.
// If runnable is running during invocation AsInstrumented panics.
// NOTE(bwplotka): Caller is expected to discard passed `r` runnable and use returned InstrumentedRunnable.Runnable instead.
func AsInstrumented(r e2e.Runnable, instrumentedPortName string, opts ...InstrumentedOption) *InstrumentedRunnable {
	if r.IsRunning() {
		panic("can't use AsInstrumented with running runnable")
	}

	opt := rOpt{
		metricPath: "/metrics",
		scheme:     "http",
		waitBackoff: backoff.New(context.Background(), backoff.Config{
			Min:        300 * time.Millisecond,
			Max:        600 * time.Millisecond,
			MaxRetries: 50, // Sometimes the CI is slow ¯\_(ツ)_/¯
		})}
	for _, o := range opts {
		o(&opt)
	}

	if r.InternalEndpoint(instrumentedPortName) == "" {
		return &InstrumentedRunnable{Runnable: e2e.NewFailedRunnable(
			r.Name(),
			errors.Newf("metric port name %v does not exists in given runnable ports", instrumentedPortName)),
		}
	}

	instr := &InstrumentedRunnable{
		Runnable:       r,
		metricPortName: instrumentedPortName,
		metricPath:     opt.metricPath,
		scheme:         opt.scheme,
		waitBackoff:    opt.waitBackoff,
	}
	r.SetMetadata(metaKey, Instrumented(instr))
	return instr
}

func (r *InstrumentedRunnable) MetricTargets() []Target {
	return []Target{{Scheme: r.scheme, MetricPath: r.metricPath, InternalEndpoint: r.InternalEndpoint(r.metricPortName)}}
}

func (r *InstrumentedRunnable) Metrics() (_ string, err error) {
	if !r.IsRunning() {
		return "", errors.Newf("%s is not running", r.Name())
	}

	// Fetch metrics.
	res, err := (&http.Client{Timeout: 5 * time.Second}).Get(fmt.Sprintf("http://%s/metrics", r.Endpoint(r.metricPortName)))
	if err != nil {
		return "", err
	}

	// Check the status code.
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", errors.Newf("unexpected status code %d while fetching metrics", res.StatusCode)
	}
	defer errcapture.ExhaustClose(&err, res.Body, "metrics response")

	body, err := io.ReadAll(res.Body)
	return string(body), err
}

func (r *InstrumentedRunnable) buildMetricsOptions(opts []MetricsOption) metricsOptions {
	result := metricsOptions{
		getValue:    getMetricValue,
		waitBackoff: r.waitBackoff,
	}
	for _, opt := range opts {
		opt(&result)
	}
	return result
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
		options = r.buildMetricsOptions(opts)
	)

	for options.waitBackoff.Reset(); options.waitBackoff.Ongoing(); {
		sums, err = r.SumMetrics(metricNames, opts...)
		if options.waitMissingMetrics && errors.Is(err, errMissingMetric) {
			options.waitBackoff.Wait()
			continue
		}
		if err != nil {
			return err
		}

		if expected(sums...) {
			return nil
		}
		options.waitBackoff.Wait()
	}
	return errors.Newf("unable to find metrics %s with expected values after %d retries. Last error: %v. Last values: %v", metricNames, options.waitBackoff.NumRetries(), err, sums)
}

// SumMetrics returns the sum of the values of each given metric names.
func (r *InstrumentedRunnable) SumMetrics(metricNames []string, opts ...MetricsOption) ([]float64, error) {
	options := r.buildMetricsOptions(opts)
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
	options := r.buildMetricsOptions(opts)

	for options.waitBackoff.Reset(); options.waitBackoff.Ongoing(); {
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

		options.waitBackoff.Wait()
	}

	return errors.Newf("the metric %s is still exported by %s", metricName, r.Name())
}
