// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e

import (
	"testing"

	"github.com/efficientgo/core/errors"
	"github.com/efficientgo/core/testutil"
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

func TestValidateName(t *testing.T) {
	for _, tcase := range []struct {
		name        string
		expectedErr error
	}{
		{
			name:        "",
			expectedErr: errors.New("name can have only ^[-a-zA-Z\\d]{1,16}$ characters due to docker network name constraints, got: "),
		},
		{
			name: "e2e-testName",
		},
		{
			name:        "e2e_testName",
			expectedErr: errors.New("name can have only ^[-a-zA-Z\\d]{1,16}$ characters due to docker network name constraints, got: e2e_testName"),
		},
		{
			name:        "e2e_testNamelongerthanexpected",
			expectedErr: errors.New("name can have only ^[-a-zA-Z\\d]{1,16}$ characters due to docker network name constraints, got: e2e_testNamelongerthanexpected"),
		},
		{
			name: func() string {
				n, err := generateName()
				testutil.Ok(t, err)
				return n
			}(),
		},
	} {
		t.Run("", func(t *testing.T) {
			err := validateName(tcase.name)
			if tcase.expectedErr != nil {
				testutil.NotOk(t, err)
				testutil.Equals(t, tcase.expectedErr.Error(), err.Error())
				return
			}

			testutil.Ok(t, err)
		})
	}
}
