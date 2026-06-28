package redisclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	Addr         string
	Password     string
	DB           int
	PoolSize     int
	MinIdleConns int
}

type Manager struct {
	mu      sync.RWMutex
	clients map[string]*redis.Client
}

const (
	defaultDialTimeout  = 10 * time.Second
	defaultReadTimeout  = 10 * time.Second
	defaultWriteTimeout = 10 * time.Second
	defaultPoolTimeout  = 10 * time.Second
)

func New(ctx context.Context, cfg Config) (*redis.Client, error) {
	options := &redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     positiveOrDefault(cfg.PoolSize, 20),
		MinIdleConns: positiveOrDefault(cfg.MinIdleConns, 5),
		DialTimeout:  defaultDialTimeout,
		ReadTimeout:  defaultReadTimeout,
		WriteTimeout: defaultWriteTimeout,
		PoolTimeout:  defaultPoolTimeout,
	}

	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return client, nil
}

func NewManager(ctx context.Context, configs map[string]Config) (*Manager, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("redis configs cannot be empty")
	}

	manager := &Manager{
		clients: make(map[string]*redis.Client, len(configs)),
	}
	for name, cfg := range configs {
		if name == "" {
			_ = manager.Close()
			return nil, fmt.Errorf("redis instance name cannot be empty")
		}
		client, err := New(ctx, cfg)
		if err != nil {
			_ = manager.Close()
			return nil, fmt.Errorf("connect redis %q: %w", name, err)
		}
		manager.clients[name] = client
	}
	return manager, nil
}

func (m *Manager) Get(name string) *redis.Client {
	if m == nil {
		panic("redis manager is nil")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	client, ok := m.clients[name]
	if !ok {
		panic(fmt.Sprintf("redis instance %q not found", name))
	}
	return client
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for name, client := range m.clients {
		if err := Close(client); err != nil {
			errs = append(errs, fmt.Errorf("close redis %q: %w", name, err))
		}
		delete(m.clients, name)
	}
	return errors.Join(errs...)
}

func Close(client *redis.Client) error {
	if client == nil {
		return nil
	}
	return client.Close()
}

func positiveOrDefault(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
