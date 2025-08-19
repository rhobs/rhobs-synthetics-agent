package version

import (
	"testing"
)

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Error("Version should not be empty")
	}
	
	// Version should match the default or build-time injected value
	t.Logf("Version: %s", Version)
}