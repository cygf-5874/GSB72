package ratelimit

import (
	"context"
	"testing"
	"time"
)

// 既有用例：只覆盖单 goroutine、时间只前进的路径。
// 并发、时钟回退、ctx 取消这些场景不在本文件的覆盖范围内。

func newTestLimiter(t *testing.T, rate, burst float64) (*Limiter, *FakeClock) {
	t.Helper()
	clock := NewFakeClock(time.Unix(1700000000, 0))
	return New(Config{Rate: rate, Burst: burst, Clock: clock}), clock
}

func TestNewBucketIsFull(t *testing.T) {
	l, _ := newTestLimiter(t, 10, 5)
	if got := l.Tokens(); got != 5 {
		t.Fatalf("新建的桶应为满：期望 5 实际 %v", got)
	}
}

func TestAllowConsumesOneToken(t *testing.T) {
	l, _ := newTestLimiter(t, 10, 5)
	if !l.Allow() {
		t.Fatalf("空桶第一次取令牌应成功")
	}
	if got := l.Tokens(); got != 4 {
		t.Fatalf("取一次后应剩 4 个令牌：实际 %v", got)
	}
}

func TestAllowUntilExhausted(t *testing.T) {
	l, _ := newTestLimiter(t, 10, 3)
	for i := 0; i < 3; i++ {
		if !l.Allow() {
			t.Fatalf("第 %d 次取令牌应成功", i+1)
		}
	}
	if l.Allow() {
		t.Fatalf("令牌用尽后应被拒绝")
	}
}

func TestRefillAfterAdvance(t *testing.T) {
	l, clock := newTestLimiter(t, 10, 10)
	if !l.Allow() {
		t.Fatalf("取令牌失败")
	}
	clock.Advance(time.Second)
	if got := l.Tokens(); got != 10 {
		t.Fatalf("前进 1 秒后应补满到 10：实际 %v", got)
	}
}

func TestBurstCapNotExceeded(t *testing.T) {
	l, clock := newTestLimiter(t, 100, 5)
	clock.Advance(time.Hour)
	if got := l.Tokens(); got != 5 {
		t.Fatalf("长时间前进后令牌数不应超过桶容量：期望 5 实际 %v", got)
	}
}

func TestDeniedReturnsFalse(t *testing.T) {
	l, _ := newTestLimiter(t, 1, 1)
	if !l.Allow() {
		t.Fatalf("第一次取令牌应成功")
	}
	if l.Allow() {
		t.Fatalf("没有令牌时应返回 false")
	}
}

func TestIssuedCountsEveryCall(t *testing.T) {
	l, _ := newTestLimiter(t, 0, 2)
	for i := 0; i < 5; i++ {
		l.Allow()
	}
	if got := l.Stats().Issued; got != 5 {
		t.Fatalf("Issued 应等于调用次数：期望 5 实际 %d", got)
	}
}

func TestAllowedAndDeniedSum(t *testing.T) {
	l, _ := newTestLimiter(t, 0, 3)
	for i := 0; i < 7; i++ {
		l.Allow()
	}
	st := l.Stats()
	if st.Allowed+st.Denied != st.Issued {
		t.Fatalf("Allowed+Denied 应等于 Issued：%+v", st)
	}
}

func TestTokensWithinBurst(t *testing.T) {
	l, clock := newTestLimiter(t, 7, 9)
	for i := 0; i < 20; i++ {
		l.Allow()
		clock.Advance(200 * time.Millisecond)
		if got := l.Tokens(); got > 9 {
			t.Fatalf("第 %d 轮令牌数超过桶容量：%v", i+1, got)
		}
	}
}

func TestWaitSucceedsWhenClockAdvances(t *testing.T) {
	l, _ := newTestLimiter(t, 10, 1)
	if !l.Allow() {
		t.Fatalf("第一次取令牌应成功")
	}
	ctx := context.Background()
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("等待并取得令牌应成功：%v", err)
	}
}

func TestNilClockFallsBackToSystem(t *testing.T) {
	l := New(Config{Rate: 1000, Burst: 4})
	if got := l.Tokens(); got > 4 {
		t.Fatalf("使用真实时钟时令牌数也不应超过桶容量：%v", got)
	}
}

func TestZeroRateKeepsOnlyInitialBurst(t *testing.T) {
	l, clock := newTestLimiter(t, 0, 2)
	clock.Advance(10 * time.Second)
	ok := 0
	for i := 0; i < 4; i++ {
		if l.Allow() {
			ok++
		}
	}
	if ok != 2 {
		t.Fatalf("速率 0 时只有初始的 2 个令牌：实际放行 %d", ok)
	}
}

func TestProportionalRefill(t *testing.T) {
	l, clock := newTestLimiter(t, 2, 100)
	for i := 0; i < 10; i++ {
		l.Allow()
	}
	clock.Advance(3 * time.Second)
	if got := l.Tokens(); got != 96 {
		t.Fatalf("速率为 2 时前进 3 秒应补 6 个：期望 96 实际 %v", got)
	}
}
