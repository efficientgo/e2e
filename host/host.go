package host

import (
	"bytes"
	"os"
	"runtime"
)

// OSPlatform returns the host's OS platform akin to `runtime.GOOS`, with
// added awareness of Windows Subsystem for Linux (WSL) 2 environments.
// The possible values are the same as `runtime.GOOS`, plus "WSL2".
func OSPlatform() string {
	if isWSL2() {
		return "WSL2"
	}
	return runtime.GOOS
}

func isWSL2() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	version, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return bytes.Contains(version, []byte("WSL2"))
}
