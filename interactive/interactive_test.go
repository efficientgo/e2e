// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2einteractive

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/efficientgo/core/errors"
	"github.com/efficientgo/core/runutil"
	"github.com/efficientgo/core/testutil"
)

func TestRunUntilEndpointHit_Signal(t *testing.T) {
	s := make(chan struct{})
	go func() {
		testutil.Ok(t, RunUntilEndpointHit())
		s <- struct{}{}
		close(s)
	}()

	pid := os.Getpid()
	testutil.Ok(t, runutil.Retry(200*time.Millisecond, context.Background().Done(), func() error {
		testutil.Ok(t, syscall.Kill(pid, syscall.SIGHUP))
		select {
		case <-s:
			return nil
		default:
			return errors.New("execution is still stopped")
		}
	}))
}

func TestRunUtilSignal(t *testing.T) {
	s := make(chan struct{})
	go func() {
		RunUntilSignal()
		s <- struct{}{}
		close(s)
	}()

	pid := os.Getpid()
	testutil.Ok(t, runutil.Retry(200*time.Millisecond, context.Background().Done(), func() error {
		testutil.Ok(t, syscall.Kill(pid, syscall.SIGHUP))
		select {
		case <-s:
			return nil
		default:
			return errors.New("execution is still stopped")
		}
	}))
}
