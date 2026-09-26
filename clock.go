package ratelimit

import (
	"sync"
	"time"
)

// Clock 抽象时间来源。限流器的全部时间读取与等待都必须经过它，
// 这样测试可以用 FakeClock 精确驱动，不依赖墙钟快慢。
type Clock interface {
	// Now 返回当前时间。
	Now() time.Time
	// Sleep 阻塞 d；FakeClock 下不真的阻塞，只是把时间往前推。
	Sleep(d time.Duration)
}

// systemClock 是默认时钟，直接使用标准库。
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) Sleep(d time.Duration) {
	if d > 0 {
		time.Sleep(d)
	}
}

// FakeClock 是可精确驱动的时钟：只有 Advance 与 Sleep 会让它前进，
// 并且它记录下每一次 Sleep 的时长，便于断言「等待序列」而不是墙钟。
//
// FakeClock 本身是并发安全的。
type FakeClock struct {
	mu    sync.Mutex
	now   time.Time
	slept []time.Duration
}

// NewFakeClock 新建一个停在 start 的假时钟。
func NewFakeClock(start time.Time) *FakeClock {
	return &FakeClock{now: start}
}

// Now 返回当前假时间。
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance 把假时间推进 d。允许传负数，用来模拟时钟回退。
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Sleep 把假时间推进 d 并记录这次等待（d <= 0 时只记录、不前进）。
func (c *FakeClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d > 0 {
		c.now = c.now.Add(d)
	}
	c.slept = append(c.slept, d)
}

// Slept 返回被记录下来的等待序列的副本。
func (c *FakeClock) Slept() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]time.Duration, len(c.slept))
	copy(out, c.slept)
	return out
}

// ResetSlept 清空等待记录。
func (c *FakeClock) ResetSlept() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.slept = nil
}
