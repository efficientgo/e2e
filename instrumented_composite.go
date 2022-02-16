// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e

import (
	"context"
	"time"

	"github.com/efficientgo/tools/core/pkg/backoff"
	"github.com/pkg/errors"
)

// CompositeInstrumentedRunnable abstract an higher-level service composed by more than one InstrumentedRunnable.
type CompositeInstrumentedRunnable struct {
	runnables []InstrumentedRunnable

	// Generic retry backoff.
	backoff *backoff.Backoff
}

func NewCompositeInstrumentedRunnable(runnables ...InstrumentedRunnable) *CompositeInstrumentedRunnable {
	return &CompositeInstrumentedRunnable{
		runnables: runnables,
		backoff: backoff.New(context.Background(), backoff.Config{
			Min:        300 * time.Millisecond,
			Max:        600 * time.Millisecond,
			MaxRetries: 50, // Sometimes the CI is slow ¯\_(ツ)_/¯
		}),
	}
}

func (r *CompositeInstrumentedRunnable) Instances() []InstrumentedRunnable {
	return r.runnables
}

func (r *CompositeInstrumentedRunnable) MetricTargets() (ret []MetricTarget) {
	for _, inst := range r.runnables {
		ret = append(ret, inst.MetricTargets()...)
	}
	return ret
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
		options = buildMetricsOptions(opts)
	)

	for r.backoff.Reset(); r.backoff.Ongoing(); {
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

		r.backoff.Wait()
	}

	return errors.Errorf("unable to find metrics %s with expected values. Last error: %v. Last values: %v", metricNames, err, sums)
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
			return nil, errors.Errorf("unexpected mismatching sum metrics results (got %d, expected %d)", len(partials), len(sums))
		}

		for i := 0; i < len(sums); i++ {
			sums[i] += partials[i]
		}
	}

	return sums, nil
}
