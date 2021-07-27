// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2einteractive

import (
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sync"

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

func RunUntilEndpointHit() (err error) {
	once := sync.Once{}
	wg := sync.WaitGroup{}

	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() {
			wg.Done()
		})
	})}

	wg.Add(1)
	go func() {
		if serr := srv.Serve(l); serr != nil {
			once.Do(func() {
				err = errors.Wrap(serr, "unexpected error")
				wg.Done()
			})
		}
	}()

	fmt.Println("Waiting for user HTTP request on ", "http://"+l.Addr().String(), "...")
	wg.Wait()
	_ = l.Close()
	return nil
}
