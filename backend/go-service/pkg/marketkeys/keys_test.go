package marketkeys

import "testing"

func TestKeysUseExistingRedisProtocol(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "kline",
			got:  KlineKey("binance", "um", "ETHUSDT", "3m"),
			want: "bn:um:k:ETHUSDT:3m",
		},
		{
			name: "indicator",
			got:  IndicatorKey("binance", "um", "ETHUSDT", "3m"),
			want: "bn:um:ind:ETHUSDT:3m",
		},
		{
			name: "indicator last",
			got:  IndicatorLastKey("binance", "um", "ETHUSDT", "3m"),
			want: "bn:um:ind:last:ETHUSDT:3m",
		},
		{
			name: "indicator history",
			got:  IndicatorHistoryKey("binance", "um", "ETHUSDT", "3m"),
			want: "bn:um:ind:hist:ETHUSDT:3m",
		},
		{
			name: "indicator window",
			got:  IndicatorWindowKey("binance", "um", "ETHUSDT", "3m"),
			want: "bn:um:indwin:ETHUSDT:3m",
		},
		{
			name: "indicator window latest",
			got:  IndicatorWindowLatestKey("binance", "um", "ETHUSDT", "3m"),
			want: "bn:um:indwin:latest:ETHUSDT:3m",
		},
		{
			name: "indicator window last",
			got:  IndicatorWindowLastKey("binance", "um", "ETHUSDT", "3m"),
			want: "bn:um:indwin:last:ETHUSDT:3m",
		},
		{
			name: "indicator realtime",
			got:  IndicatorRealtimeKey("binance", "um", "ETHUSDT", "3m"),
			want: "bn:um:indrt:ETHUSDT:3m",
		},
		{
			name: "data health",
			got:  DataHealthKey("binance", "um", "ETHUSDT", "3m"),
			want: "bn:um:health:ETHUSDT:3m",
		},
		{
			name: "last price",
			got:  LastPriceKey("binance", "um", "ETHUSDT"),
			want: "bn:um:lp:ETHUSDT",
		},
		{
			name: "mark price",
			got:  MarkPriceKey("binance", "um", "ETHUSDT"),
			want: "bn:um:mp:ETHUSDT",
		},
		{
			name: "non binance exchange",
			got:  IndicatorRealtimeKey("gate", "um", "ETH_USDT", "3m"),
			want: "gate:um:indrt:ETH_USDT:3m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("key = %q, want %q", tt.got, tt.want)
			}
		})
	}
}
