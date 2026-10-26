package chrono

import (
	"os"
	"testing"
)

// Tests run in UTC unless told otherwise, the way `bun test` does — the
// corpus was generated there. time.Local is read lazily, so this lands first.
func TestMain(m *testing.M) {
	if os.Getenv("TZ") == "" {
		os.Setenv("TZ", "UTC")
	}
	os.Exit(m.Run())
}
