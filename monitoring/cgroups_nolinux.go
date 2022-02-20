// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

//go:build !linux
// +build !linux

package e2emonitoring

import (
	"github.com/efficientgo/e2e"
	"github.com/pkg/errors"
)

func setupPIDAsContainer(_ e2e.Environment, _ e2e.Runnable, _ int) error {
	return nil, errors.New("not implemented for non Linux system")
}
