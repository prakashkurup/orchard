package update

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.2.0", "v0.3.0", true},
		{"0.2.0", "0.3.0", true},
		{"v0.3.0", "v0.3.0", false},
		{"v0.3.1", "v0.3.0", false},
		{"v0.3.0", "v1.0.0", true},
		{"v1.2.3", "v1.2.4", true},
		{"dev", "v0.3.0", false},        // dev builds are never nagged
		{"v0.3.0", "garbage", false},    // unparseable latest
		{"v0.3.0", "v0.3.0-rc1", false}, // prerelease of same version is not newer
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q,%q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestCurrentPrefersLdflag(t *testing.T) {
	if got := Current("v0.3.0"); got != "v0.3.0" {
		t.Errorf("Current(ldflag) = %q, want v0.3.0", got)
	}
	// "dev" falls through to build info (or "dev" in tests); must not be empty
	if got := Current("dev"); got == "" {
		t.Error("Current(dev) should not be empty")
	}
}

func TestVerify(t *testing.T) {
	data := []byte("hello orchard")
	sum := sha256.Sum256(data)
	good := hex.EncodeToString(sum[:]) + "  orchard_0.3.0_darwin_arm64.tar.gz\n"
	if err := verify(data, "orchard_0.3.0_darwin_arm64.tar.gz", good); err != nil {
		t.Errorf("matching checksum should pass: %v", err)
	}
	bad := "deadbeef  orchard_0.3.0_darwin_arm64.tar.gz\n"
	if err := verify(data, "orchard_0.3.0_darwin_arm64.tar.gz", bad); err == nil {
		t.Error("mismatched checksum should fail")
	}
	// name absent from checksums -> skipped, not an error
	if err := verify(data, "orchard_0.3.0_linux_amd64.tar.gz", good); err != nil {
		t.Errorf("absent name should skip, got %v", err)
	}
}

func TestBaseName(t *testing.T) {
	if got := baseName("https://x/y/orchard_0.3.0_darwin_arm64.tar.gz"); got != "orchard_0.3.0_darwin_arm64.tar.gz" {
		t.Errorf("baseName = %q", got)
	}
}
