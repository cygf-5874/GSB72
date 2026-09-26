package ratelimit

import (
	"context"
	"time"
)

// Config 描述一个令牌桶。
type Config struct {
	// Rate 是每秒补充的令牌数，必须大于 0。
	Rate float64
	// Burst 是桶容量，必须大于 0。新建的桶是满的。
	Burst float64
	// Clock 是时间来源；为 nil 时使用真实时钟。
	Clock Clock
}

// Limiter 是一个令牌桶限流器。
type Limiter struct {
	cfg Config

	tokens float64
	last   time.Time

	issued  int64
	allowed int64
	denied  int64
}

// New 新建一个装满令牌的限流器。
func New(cfg Config) *Limiter {
	if cfg.Clock == nil {
		cfg.Clock = systemClock{}
	}
	return &Limiter{
		cfg:    cfg,
		tokens: cfg.Burst,
		last:   cfg.Clock.Now(),
	}
}

// refill 按「从上一次补充到现在」的流逝时间补充令牌，并把令牌数裁剪到桶容量。
func (l *Limiter) refill() {
	now := l.cfg.Clock.Now()
	elapsed := now.Sub(l.last).Seconds()
	l.tokens += elapsed * l.cfg.Rate
	l.last = now
	if l.tokens > l.cfg.Burst {
		l.tokens = l.cfg.Burst
	}
}

// Tokens 返回当前可用的令牌数。它是只读的，不会改变限流器的状态。
func (l *Limiter) Tokens() float64 {
	t := l.tokens + l.cfg.Clock.Now().Sub(l.last).Seconds()*l.cfg.Rate
	if t > l.cfg.Burst {
		t = l.cfg.Burst
	}
	return t
}

// Allow 尝试取走一个令牌：取到返回 true，没有令牌返回 false。
func (l *Limiter) Allow() bool {
	l.refill()

	l.issued++
	l.tokens--
	if l.tokens < 0 {
		l.denied++
		return false
	}
	l.allowed++
	return true
}

// Wait 一直等到取到一个令牌为止。
//
// 契约：ctx 取消时立即返回 ctx.Err()，并且不消耗任何令牌。
func (l *Limiter) Wait(ctx context.Context) error {
	if l.cfg.Rate <= 0 {
		return ErrNotAvailable
	}
	interval := time.Duration(float64(time.Second) / l.cfg.Rate)
	l.cfg.Clock.Sleep(interval)
	if l.Allow() {
		return nil
	}
	return ErrNotAvailable
}
