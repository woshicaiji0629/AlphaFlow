package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var incrementScript = goredis.NewScript(`
local value = redis.call('INCR', KEYS[1])
if value == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return value
`)

type LoginLimiter struct {
	client          *goredis.Client
	window          time.Duration
	maxEmail, maxIP int64
}

func NewLoginLimiter(client *goredis.Client, window time.Duration, maxEmail, maxIP int) (*LoginLimiter, error) {
	if client == nil || window <= 0 || maxEmail <= 0 || maxIP <= 0 {
		return nil, fmt.Errorf("invalid login limiter configuration")
	}
	return &LoginLimiter{client: client, window: window, maxEmail: int64(maxEmail), maxIP: int64(maxIP)}, nil
}

func (l *LoginLimiter) Allow(ctx context.Context, email, ip string) (bool, error) {
	emailCount, err := l.increment(ctx, "email", strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return false, err
	}
	ipCount, err := l.increment(ctx, "ip", strings.TrimSpace(ip))
	if err != nil {
		return false, err
	}
	return emailCount <= l.maxEmail && ipCount <= l.maxIP, nil
}

func (l *LoginLimiter) ResetEmail(ctx context.Context, email string) error {
	return l.client.Del(ctx, key("email", strings.ToLower(strings.TrimSpace(email)))).Err()
}
func (l *LoginLimiter) increment(ctx context.Context, kind, value string) (int64, error) {
	if value == "" {
		value = "unknown"
	}
	result, err := incrementScript.Run(ctx, l.client, []string{key(kind, value)}, l.window.Milliseconds()).Int64()
	if err != nil {
		return 0, fmt.Errorf("increment login limit: %w", err)
	}
	return result, nil
}
func key(kind, value string) string {
	sum := sha256.Sum256([]byte(value))
	return "af:control:login:" + kind + ":" + hex.EncodeToString(sum[:])
}
