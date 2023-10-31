// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2emon

import (
	"context"
	"time"

	"github.com/efficientgo/core/backoff"
	"github.com/efficientgo/core/errors"
)

// CompositeInstrumentedRunnable abstract a higher-level service composed by more than one InstrumentedRunnable.
type CompositeInstrumentedRunnable struct {
	runnables []*InstrumentedRunnable

	// Generic retry backoff.
	backoff *backoff.Backoff
}

func NewCompositeInstrumentedRunnable(runnables ...*InstrumentedRunnable) *CompositeInstrumentedRunnable {
	return &CompositeInstrumentedRunnable{
		runnables: runnables,
		backoff: backoff.New(context.Background(), backoff.Config{
			Min:        300 * time.Millisecond,
			Max:        600 * time.Millisecond,
			MaxRetries: 50, // Sometimes the CI is slow ¯\_(ツ)_/¯
		}),
	}
}

func (r *CompositeInstrumentedRunnable) Instances() []*InstrumentedRunnable {
	return r.runnables
}

func (r *CompositeInstrumentedRunnable) MetricTargets() (ret []Target) {
	for _, inst := range r.runnables {
		ret = append(ret, inst.MetricTargets()...)
	}
	return ret
}

func (r *CompositeInstrumentedRunnable) buildMetricsOptions(opts []MetricsOption) metricsOptions {
	result := metricsOptions{
		getValue:    getMetricValue,
		waitBackoff: r.backoff,
	}
	for _, opt := range opts {
		opt(&result)
	}
	return result
}

// WaitSumMetrics waits for at least one instance of each given metric names to be present and their sums, returning true
// when passed to given expected(...).
func (r *CompositeInstrumentedRunnable) WaitSumMetrics(expected MetricValueExpectation, metricNames ...string) error {
	return r.WaitSumMetricsWithOptions(expected, metricNames)
}

func (r *CompositeInstrumentedRunnable) WaitSumMetricsWithOptions(expected MetricValueExpectation, metricNames []string, opts ...MetricsOption) error {
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

	return errors.Wrapf(err, "unable to find metrics %s with expected values. Last values: %v", metricNames, sums)
}

// SumMetrics returns the sum of the values of each given metric names.
func (r *CompositeInstrumentedRunnable) SumMetrics(metricNames []string, opts ...MetricsOption) ([]float64, error) {
	sums := make([]float64, len(metricNames))

	for _, service := range r.runnables {
		partials, err := service.SumMetrics(metricNames, opts...)
		if err != nil {
			return nil, err
		}

		if len(partials) != len(sums) {
			return nil, errors.Newf("unexpected mismatching sum metrics results (got %d, expected %d)", len(partials), len(sums))
		}

		for i := 0; i < len(sums); i++ {
			sums[i] += partials[i]
		}
	}

	return sums, nil
}
