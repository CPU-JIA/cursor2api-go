// Copyright (c) 2025-2026 libaxuan
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package middleware

import (
	"cursor2api-go/models"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

const (
	defaultRateLimitRPM      = 60
	rateLimitCleanupInterval = 10 * time.Minute
	rateLimitEntryTTL        = 30 * time.Minute
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type ipRateLimiter struct {
	mu          sync.Mutex
	visitors    map[string]*visitor
	rpm         int
	lastCleanup time.Time
}

func newIPRateLimiter(rpm int) *ipRateLimiter {
	return &ipRateLimiter{
		visitors:    make(map[string]*visitor),
		rpm:         rpm,
		lastCleanup: time.Now(),
	}
}

func (l *ipRateLimiter) get(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if now.Sub(l.lastCleanup) >= rateLimitCleanupInterval {
		l.cleanup(now)
	}

	entry, exists := l.visitors[ip]
	if !exists {
		entry = &visitor{
			limiter:  rate.NewLimiter(rate.Every(time.Minute/time.Duration(l.rpm)), l.rpm),
			lastSeen: now,
		}
		l.visitors[ip] = entry
	} else {
		entry.lastSeen = now
	}

	return entry.limiter
}

func (l *ipRateLimiter) cleanup(now time.Time) {
	for ip, entry := range l.visitors {
		if now.Sub(entry.lastSeen) >= rateLimitEntryTTL {
			delete(l.visitors, ip)
		}
	}
	l.lastCleanup = now
}

// RateLimit 请求限流中间件
func RateLimit() gin.HandlerFunc {
	rpm := getRateLimitRPM()
	store := newIPRateLimiter(rpm)

	return func(c *gin.Context) {
		limiter := store.get(c.ClientIP())
		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, models.NewErrorResponse(
				"Rate limit exceeded",
				"rate_limit_error",
				"rate_limit_exceeded",
			))
			c.Abort()
			return
		}

		c.Next()
	}
}

func getRateLimitRPM() int {
	value := strings.TrimSpace(os.Getenv("RATE_LIMIT_RPM"))
	if value == "" {
		return defaultRateLimitRPM
	}

	rpm, err := strconv.Atoi(value)
	if err != nil || rpm <= 0 {
		logrus.Warnf("Invalid RATE_LIMIT_RPM value: %s, using default: %d", value, defaultRateLimitRPM)
		return defaultRateLimitRPM
	}

	return rpm
}
