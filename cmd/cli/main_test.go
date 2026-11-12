package main

import (
	"strings"
	"testing"
)

func TestHistorySeparator(t *testing.T) {
	separator := historySeparator()
	if separator != strings.Repeat("-", 100) {
		t.Fatalf("history separator must be 100 visible dashes, got %q", separator)
	}
	if strings.ContainsRune(separator, '\x00') {
		t.Fatal("history separator contains NUL bytes")
	}
}
