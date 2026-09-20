//go:build !darwin && !linux

package service

import (
	"context"
	"errors"
	"runtime"
)

var errUnsupported = errors.New("oku cannot manage services on " + runtime.GOOS + " yet")

type unsupported struct{}

// New returns the service manager for this OS.
func New(_, _ string) Manager { return unsupported{} }

func (unsupported) Install(context.Context, Definition, bool) error { return errUnsupported }
func (unsupported) Remove(context.Context, Definition) error        { return nil }
func (unsupported) Start(context.Context, Definition) error         { return errUnsupported }
func (unsupported) Stop(context.Context, Definition) error          { return errUnsupported }
func (unsupported) File(Definition) string                          { return "" }

func (unsupported) Status(context.Context, Definition) (Status, error) {
	return Status{}, errUnsupported
}

func (unsupported) Logs(context.Context, Definition, int) (string, error) {
	return "", errUnsupported
}
