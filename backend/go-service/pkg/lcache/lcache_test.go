package lcache

import (
	"testing"
	"time"
)

func TestCacheGetSetEx(t *testing.T) {
	cache := MustNew(10)

	cache.SetEx("key", "value", time.Minute)
	value, ok := cache.Get("key")
	if !ok {
		t.Fatal("Get missed")
	}
	if value != "value" {
		t.Fatalf("value = %v, want value", value)
	}
}

func TestCacheExpires(t *testing.T) {
	cache := MustNew(10)

	cache.SetEx("key", "value", time.Millisecond)
	time.Sleep(2 * time.Millisecond)

	if _, ok := cache.Get("key"); ok {
		t.Fatal("Get hit expired key")
	}
}

func TestCacheFreqCall(t *testing.T) {
	cache := MustNew(10)
	calls := 0

	cache.FreqCall("key", time.Minute, func() {
		calls++
	})
	cache.FreqCall("key", time.Minute, func() {
		calls++
	})
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestCacheFreqCallAfterExpiry(t *testing.T) {
	cache := MustNew(10)
	calls := 0

	cache.FreqCall("key", time.Millisecond, func() {
		calls++
	})
	time.Sleep(2 * time.Millisecond)
	cache.FreqCall("key", time.Millisecond, func() {
		calls++
	})
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestCacheLRUEvicts(t *testing.T) {
	cache := MustNew(2)

	cache.SetEx("oldest", true, time.Minute)
	cache.SetEx("middle", true, time.Minute)
	cache.SetEx("newest", true, time.Minute)

	if _, ok := cache.Get("oldest"); ok {
		t.Fatal("oldest still exists")
	}
	if _, ok := cache.Get("newest"); !ok {
		t.Fatal("newest missed")
	}
}
