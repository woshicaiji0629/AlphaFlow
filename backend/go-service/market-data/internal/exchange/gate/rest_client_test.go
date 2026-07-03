package gate

import "testing"

func TestRESTClientWrapper(t *testing.T) {
	client := NewRESTClient("https://example.test", "usdt", nil)
	if client.Exchange() != "gate" {
		t.Fatalf("exchange = %q, want gate", client.Exchange())
	}
	if client.Market() != "usdt" {
		t.Fatalf("market = %q, want usdt", client.Market())
	}
}
