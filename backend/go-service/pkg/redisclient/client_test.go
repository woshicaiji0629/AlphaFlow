package redisclient

import (
	"testing"

	"alphaflow/go-service/pkg/constants"
	"github.com/redis/go-redis/v9"
)

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

func TestManagerGetPanicsWhenMissing(t *testing.T) {
	manager := &Manager{clients: map[string]*redis.Client{}}

	defer func() {
		if recover() == nil {
			t.Fatal("expected Get to panic when instance is missing")
		}
	}()
	_ = manager.Get("missing")
}

func TestManagerGetReturnsClient(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	manager := &Manager{
		clients: map[string]*redis.Client{
			constants.RedisDefaultInstance: client,
		},
	}

	if got := manager.Get(constants.RedisDefaultInstance); got != client {
		t.Fatal("Get did not return default client")
	}
}

func TestManagerCloseNil(t *testing.T) {
	var manager *Manager

	if err := manager.Close(); err != nil {
		t.Fatalf("Close nil manager: %v", err)
	}
}
