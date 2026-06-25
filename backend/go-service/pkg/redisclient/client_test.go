package redisclient

import "testing"

func TestPositiveOrDefault(t *testing.T) {
	tests := map[int]int{
		0:  20,
		-1: 20,
		3:  3,
	}

	for input, want := range tests {
		if got := positiveOrDefault(input, 20); got != want {
			t.Fatalf("positiveOrDefault(%d) = %d, want %d", input, got, want)
		}
	}
}

func TestCloseNil(t *testing.T) {
	if err := Close(nil); err != nil {
		t.Fatalf("Close(nil): %v", err)
	}
}
