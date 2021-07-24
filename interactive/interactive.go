// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2einteractive

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"testing"

	"github.com/pkg/errors"
)

func OpenInBrowser(url string) error {
	fmt.Println("Opening", url, "in browser.")
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Run()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Run()
	case "darwin":
		err = exec.Command("open", url).Run()
	default:
		err = errors.Errorf("unsupported platform")
	}
	return err
}

// RunUntilInterrupt ...
// TODO(bwplotka): Comment RunUntilInterrupt, make sure it makes sense.
func RunUntilInterrupt(t testing.TB) error {
	fmt.Println("Waiting for user interrupt...")
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	sig := <-c
	return SignalError{Signal: sig}
}

// SignalError is returned by the signal handler's execute function
// when it terminates due to a received signal.
type SignalError struct {
	Signal os.Signal
}

// Error implements the error interface.
func (e SignalError) Error() string {
	return fmt.Sprintf("received signal %s", e.Signal)
}
