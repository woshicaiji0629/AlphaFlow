package lcache

import (
	"time"

	lru "github.com/hashicorp/golang-lru"
)

type Cache struct {
	cache *lru.Cache
}

type item struct {
	value     any
	expiresAt time.Time
}

func New(size int) (*Cache, error) {
	cache, err := lru.New(size)
	if err != nil {
		return nil, err
	}
	return &Cache{cache: cache}, nil
}

func MustNew(size int) *Cache {
	cache, err := New(size)
	if err != nil {
		panic(err)
	}
	return cache
}

func (c *Cache) Set(key string, value any) {
	c.SetEx(key, value, 24*time.Hour)
}

func (c *Cache) SetEx(key string, value any, exp time.Duration) {
	if c == nil || c.cache == nil {
		return
	}
	if exp <= 0 {
		return
	}
	c.cache.Add(key, item{
		value:     value,
		expiresAt: time.Now().Add(exp),
	})
}

func (c *Cache) Get(key string) (any, bool) {
	if c == nil || c.cache == nil {
		return nil, false
	}
	value, ok := c.cache.Get(key)
	if !ok {
		return nil, false
	}
	cached, ok := value.(item)
	if !ok || !cached.expiresAt.After(time.Now()) {
		c.cache.Remove(key)
		return nil, false
	}
	return cached.value, true
}

func (c *Cache) Del(key string) {
	if c == nil || c.cache == nil {
		return
	}
	c.cache.Remove(key)
}

func (c *Cache) FreqCall(key string, exp time.Duration, fn func()) {
	if _, ok := c.Get(key); ok {
		return
	}
	c.SetEx(key, true, exp)
	fn()
}
