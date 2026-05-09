//go:build !windows

package main

func enableANSI() {}

func getDiskSize(_ string, fallback int64) int64 { return fallback }
