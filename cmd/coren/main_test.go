package main

import (
	"testing"

	"coren/internal/config"
)

func TestIsLoopbackAddr(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8787":   true,
		"localhost:8787":   true,
		"[::1]:8787":       true,
		":8787":            true,
		"0.0.0.0:8787":     false,
		"192.168.1.5:8787": false,
		"example.com:80":   false,
	}
	for addr, want := range cases {
		if got := isLoopbackAddr(addr); got != want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestCheckExposure(t *testing.T) {
	// Loopback without password is allowed.
	if err := checkExposure(config.Config{Addr: "127.0.0.1:8787"}); err != nil {
		t.Fatalf("loopback should be allowed: %v", err)
	}
	// Non-loopback without password is refused.
	if err := checkExposure(config.Config{Addr: "0.0.0.0:8787"}); err == nil {
		t.Fatal("non-loopback without password should be refused")
	}
	// Non-loopback with password is allowed.
	if err := checkExposure(config.Config{Addr: "0.0.0.0:8787", Password: "pw"}); err != nil {
		t.Fatalf("non-loopback with password should be allowed: %v", err)
	}
}
