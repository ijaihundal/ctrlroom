package version

import (
	"strings"
	"testing"
)

func TestStringContainsVersion(t *testing.T) {
	if got := String(); !strings.Contains(got, Version) {
		t.Errorf("String() = %q, want substring %q", got, Version)
	}
}

func TestStringFormat(t *testing.T) {
	got := String()
	wantSuffix := "(" + Commit + "@" + Date + ")"
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("String() = %q, want suffix %q", got, wantSuffix)
	}
}
