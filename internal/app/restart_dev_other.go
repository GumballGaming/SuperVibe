//go:build !windows || !dev

package app

func scheduleSelfDelete() error { return nil }
