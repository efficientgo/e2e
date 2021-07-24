// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e

import (
	"testing"

	"github.com/efficientgo/tools/core/pkg/testutil"
	"github.com/pkg/errors"
)

func TestGetDockerPortMapping(t *testing.T) {
	for _, tcase := range []struct {
		out          string
		expectedPort int
		expectedErr  error
	}{
		{out: "0.0.0.0:2313", expectedPort: 2313},
		{out: "error", expectedErr: errors.New("got unexpected output: error")},
		{out: `        0.0.0.0:49154
        :::49154`, expectedPort: 49154},
	} {
		t.Run("", func(t *testing.T) {
			l, err := getDockerPortMapping([]byte(tcase.out))
			if tcase.expectedErr != nil {
				testutil.NotOk(t, err)
				testutil.Equals(t, tcase.expectedErr.Error(), err.Error())
				return
			}
			testutil.Ok(t, err)
			testutil.Equals(t, tcase.expectedPort, l)
		})
	}
}
