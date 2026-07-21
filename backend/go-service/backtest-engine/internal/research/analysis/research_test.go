package analysis

import (
	"strings"
	"testing"

	"alphaflow/go-service/pkg/marketregime"
)

func TestConfigFingerprintsAreStableAndVersionSpecific(t *testing.T) {
	seen := map[string]struct{}{}
	for _, version := range []marketregime.Version{marketregime.VersionV4, marketregime.VersionV5, marketregime.VersionV6} {
		first, err := configFingerprint(version)
		if err != nil {
			t.Fatal(err)
		}
		second, err := configFingerprint(version)
		if err != nil {
			t.Fatal(err)
		}
		if first != second || !strings.HasPrefix(first, "sha256:") {
			t.Fatalf("unstable %s fingerprint: %q != %q", version, first, second)
		}
		if _, ok := seen[first]; ok {
			t.Fatalf("duplicate fingerprint for %s: %q", version, first)
		}
		seen[first] = struct{}{}
	}
}
