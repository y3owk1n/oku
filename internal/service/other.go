//go:build !darwin && !linux && !windows

package service

// New returns the service manager for this OS.
func New(_, _ string) Manager { return unsupported{} }

// SystemLogDir is unused on this OS.
func SystemLogDir() string { return "" }

// CheckSystem accepts every service in system scope.
func CheckSystem(Definition) error { return nil }

// NewSystem returns the manager for services that run for the whole machine.
func NewSystem() Manager { return unsupported{} }
