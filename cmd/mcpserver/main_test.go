package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestDurationFromSeconds(t *testing.T) {
	t.Setenv("MCP_TEST_DURATION", " 7 ")
	got, err := durationFromSeconds("MCP_TEST_DURATION", time.Second)
	if err != nil || got != 7*time.Second {
		t.Fatalf("durationFromSeconds() = %v, %v; want 7s", got, err)
	}

	t.Setenv("MCP_TEST_DURATION", "")
	got, err = durationFromSeconds("MCP_TEST_DURATION", 3*time.Second)
	if err != nil || got != 3*time.Second {
		t.Fatalf("durationFromSeconds() = %v, %v; want fallback 3s", got, err)
	}

	for _, value := range []string{"invalid", "0", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("MCP_TEST_DURATION", value)
			if _, err := durationFromSeconds("MCP_TEST_DURATION", time.Second); err == nil || !strings.Contains(err.Error(), "MCP_TEST_DURATION") {
				t.Fatalf("durationFromSeconds(%q) error = %v", value, err)
			}
		})
	}
}

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("MCP_TEST_VALUE", "  configured ")
	if got := envOrDefault("MCP_TEST_VALUE", "fallback"); got != "configured" {
		t.Fatalf("envOrDefault() = %q, want configured", got)
	}

	if err := os.Unsetenv("MCP_TEST_VALUE"); err != nil {
		t.Fatal(err)
	}
	if got := envOrDefault("MCP_TEST_VALUE", "fallback"); got != "fallback" {
		t.Fatalf("envOrDefault() = %q, want fallback", got)
	}
}
