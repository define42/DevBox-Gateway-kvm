//go:build !windows

package main

func platformConnectionReset(error) bool { return false }
