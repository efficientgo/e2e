// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2einteractive

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	"github.com/efficientgo/e2e/host"

	"github.com/efficientgo/core/errors"
)

// OpenInBrowser opens given URL in your default browser.
func OpenInBrowser(url string) error {
	fmt.Println("Opening", url, "in browser.")
	var err error
	switch host.OSPlatform() {
	case "WSL2":
		err = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url).Run()
	case "linux":
		err = exec.Command("xdg-open", url).Run()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Run()
	case "darwin":
		err = exec.Command("open", url).Run()
	default:
		err = errors.New("unsupported platform")
	}
	return err
}

// RunUntilEndpointHit stalls current goroutine executions and prints the URL to local address. When URL is hit
// this function returns with nil to continue the execution. It also watches for SIGINT, SIGKILL or SIGHUP signals
// and does the same when any of those is seen.
//
// This function is useful when you want to interact with e2e tests and manually decide when to finish. Use this function
// as opposed to RunUntilSignal for certain IDEs that does not send correct signal on stop (e.g. Goland pre 2022.3,
// see https://youtrack.jetbrains.com/issue/GO-5982).
func RunUntilEndpointHit() error {
	return RunUntilEndpointHitWithPort(0)
}

// RunUntilEndpointHitWithPort is like RunUntilEndpointHit, but it allows specifying static port.
func RunUntilEndpointHitWithPort(port int) (err error) {
	once := sync.Once{}
	wg := sync.WaitGroup{}
	stopWG := sync.WaitGroup{}
	stopWG.Add(1)

	l, err := net.Listen("tcp", fmt.Sprintf("localhost:%v", port))
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { stopWG.Done() })
	})}

	wg.Add(2)
	go func() {
		if serr := srv.Serve(l); serr != nil {
			once.Do(func() {
				err = errors.Wrap(serr, "unexpected error")
				stopWG.Done()
			})
		}
		wg.Done()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		s := make(chan os.Signal, 1)
		signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		select {
		case <-s:
		case <-ctx.Done():
		}
		once.Do(func() { stopWG.Done() })
		wg.Done()
	}()

	fmt.Println("Waiting for user HTTP request on", "http://"+l.Addr().String(), " or SIGINT, SIGKILL or SIGHUP signal...")
	stopWG.Wait()

	// Cleanup.
	cancel()
	_ = l.Close()
	wg.Wait()
	return nil
}

// RunUntilSignal stops the current goroutine execution and watches for SIGINT, SIGKILL and SIGHUP signals. Once spotted it continues
// the execution. This function is useful when you want to interact with e2e tests and manually decide when to finish.
func RunUntilSignal() {
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	fmt.Println("Waiting for user SIGINT, SIGKILL or SIGHUP signal (Ctrl+C or IDE stop)")
	<-s
}
