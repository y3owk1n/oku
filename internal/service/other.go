//go:build !darwin && !linux && !windows

package service

// New returns the service manager for this OS.
func New(_, _ string) Manager { return unsupported{} }

// SystemLogDir is unused on this OS.
const SystemLogDir = ""

// NewSystem returns the manager for services that run for the whole machine.
func NewSystem() Manager { return unsupported{} }
