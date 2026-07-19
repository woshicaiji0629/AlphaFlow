package marketregime

import "testing"

func TestV6ReasonsUseIndependentVersion(t *testing.T) {
	reasons := v6Reasons([]string{"v5_breakout_width_weak", "v5_fast_release_confirmed"})
	if reasons[0] != "v6_breakout_width_weak" || reasons[1] != "v6_fast_release_confirmed" {
		t.Fatalf("reasons=%v", reasons)
	}
}

func TestVersionedAnalyzerConstructsV6(t *testing.T) {
	analyzer, err := NewAnalyzer(VersionV6, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if analyzer.Version() != VersionV6 {
		t.Fatalf("version=%q", analyzer.Version())
	}
}
