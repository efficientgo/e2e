// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2einteractive

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/efficientgo/core/errors"
	"github.com/efficientgo/core/runutil"
	"github.com/efficientgo/core/testutil"
)

func TestRunUntilEndpointHitWithPort(t *testing.T) {
	const port = 12131

	s := make(chan struct{})
	go func() {
		testutil.Ok(t, RunUntilEndpointHitWithPort(port))
		s <- struct{}{}
		close(s)
	}()

	r, err := http.Get(fmt.Sprintf("http://localhost:%v", port))
	testutil.Ok(t, err)
	testutil.Equals(t, 200, r.StatusCode)

	testutil.Ok(t, runutil.Retry(200*time.Millisecond, context.Background().Done(), func() error {
		select {
		case <-s:
			return nil
		default:
			return errors.New("execution is still stopped")
		}
	}))
}

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
