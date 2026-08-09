package main

import (
	"strings"
	"testing"
)

func TestResolveDataDir_PrefersFlagOverDefault(t *testing.T) {
	if got := resolveDataDir("/tmp/custom"); got != "/tmp/custom" {
		t.Errorf("resolveDataDir(\"/tmp/custom\") = %q, want /tmp/custom", got)
	}
	if got := resolveDataDir(""); !strings.Contains(got, "data") {
		t.Errorf("default data dir = %q, want it to contain \"data\"", got)
	}
}
