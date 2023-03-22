// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e_test

import (
	"testing"

	"github.com/efficientgo/core/testutil"
	"github.com/efficientgo/e2e"
)

func TestKindEnvironment(t *testing.T) {
	t.Parallel()

	e, err := e2e.NewKindEnvironment()
	testutil.Ok(t, err)
	t.Cleanup(e.Close)
	testEnvironment(t, e)
}
