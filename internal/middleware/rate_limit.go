package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/gin-gonic/gin"
)

type RateLimitConfig struct {
	KeyPrefix string
	Limit     int
	Window    time.Duration
}

type window struct {
	count int
	reset time.Time
}

// memoryLimiter is a fixed-window counter per key. FMS runs as one instance, so the
// counters live in memory (kratify uses Redis for the same thing).
type memoryLimiter struct {
	mu      sync.Mutex
	windows map[string]*window
}

var limiter = &memoryLimiter{windows: map[string]*window{}}

func (l *memoryLimiter) hit(key string, w time.Duration) (int, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	// Drop expired windows now and then so the map does not grow forever.
	if len(l.windows) > 10000 {
		for k, v := range l.windows {
			if now.After(v.reset) {
				delete(l.windows, k)
			}
		}
	}
	cur, ok := l.windows[key]
	if !ok || now.After(cur.reset) {
		cur = &window{reset: now.Add(w)}
		l.windows[key] = cur
	}
	cur.count++
	return cur.count, cur.reset
}

// RateLimit creates a fixed-window in-memory rate limiter per client IP.
func RateLimit(cfg RateLimitConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.Limit <= 0 || cfg.Window <= 0 {
			c.Next()
			return
		}
		count, reset := limiter.hit(cfg.KeyPrefix+":"+c.ClientIP(), cfg.Window)
		remaining := cfg.Limit - count
		if remaining < 0 {
			remaining = 0
		}
		c.Header("X-RateLimit-Limit", strconv.Itoa(cfg.Limit))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		if count > cfg.Limit {
			retry := int(time.Until(reset).Seconds())
			if retry < 1 {
				retry = 1
			}
			c.Header("Retry-After", strconv.Itoa(retry))
			response.Error(c, http.StatusTooManyRequests, "Too many requests, please try again later", nil)
			c.Abort()
			return
		}
		c.Next()
	}
}
