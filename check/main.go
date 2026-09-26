// check 是本题的固定验收程序：逐场景打印 PASS/FAIL，结尾打印通过数。
//
//	go run ./check                 # 跑全部 10 个场景
//	go run ./check -list           # 只列出场景名
//	go run ./check -only clock     # 只跑某个分组
//	go run ./check -only deny/DENY1_denial_does_not_consume
//
// 请勿修改本程序。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ratelimit"
)

const scenarioTimeout = 25 * time.Second

func baseTime() time.Time { return time.Unix(1700000000, 0) }

type scenario struct {
	group string
	name  string
	fn    func() error
}

var allScenarios = []scenario{
	{"concurrency", "CONC1_counters_exact", conc1},
	{"concurrency", "CONC2_token_conservation", conc2},
	{"concurrency", "CONC3_tokens_in_range", conc3},
	{"clock", "CLK1_backward_shift_in_range", clk1},
	{"clock", "CLK2_refill_proportional", clk2},
	{"deny", "DENY1_denial_does_not_consume", deny1},
	{"deny", "DENY2_repeated_denial_keeps_tokens", deny2},
	{"deny", "DENY3_cancelled_wait_does_not_consume", deny3},
	{"purity", "PUR1_no_wall_clock_and_fake_clock_drives", pur1},
	{"purity", "PUR2_repeatable", pur2},
}

func main() {
	var only string
	var list bool
	flag.StringVar(&only, "only", "", "只跑指定分组或场景（组名、组/名或场景名，逗号分隔）")
	flag.BoolVar(&list, "list", false, "只列出全部场景")
	flag.Parse()

	selected := selectScenarios(only)
	if len(selected) == 0 {
		fmt.Printf("没有匹配的场景：%q\n", only)
		os.Exit(2)
	}

	if list {
		for _, s := range selected {
			fmt.Printf("%s/%s\n", s.group, s.name)
		}
		return
	}

	pass := 0
	for _, s := range selected {
		err := runScenario(s)
		if err == nil {
			pass++
			fmt.Printf("PASS %s/%s\n", s.group, s.name)
			continue
		}
		fmt.Printf("FAIL %s/%s  %s\n", s.group, s.name, err.Error())
	}
	fmt.Printf("结果：通过 %d/%d\n", pass, len(selected))
	if pass != len(selected) {
		os.Exit(1)
	}
}

func selectScenarios(only string) []scenario {
	if strings.TrimSpace(only) == "" {
		return allScenarios
	}
	want := map[string]bool{}
	for _, p := range strings.Split(only, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			want[p] = true
		}
	}
	var out []scenario
	for _, s := range allScenarios {
		if want[s.group] || want[s.name] || want[s.group+"/"+s.name] {
			out = append(out, s)
		}
	}
	return out
}

func runScenario(s scenario) error {
	done := make(chan error, 1)
	go func() {
		var err error
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("期望=场景正常完成 实际=panic: %v", r)
			}
			done <- err
		}()
		err = s.fn()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(scenarioTimeout):
		return fmt.Errorf("期望=场景在 %v 内结束 实际=超时", scenarioTimeout)
	}
}

// runConcurrent 让 workers 个 goroutine 在栅栏后同时开始请求。
func runConcurrent(l *ratelimit.Limiter, workers, steps int) {
	var wg sync.WaitGroup
	entered := make(chan struct{}, workers)
	release := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entered <- struct{}{}
			<-release
			for j := 0; j < steps; j++ {
				l.Allow()
			}
		}()
	}
	for i := 0; i < workers; i++ {
		<-entered
	}
	close(release)
	wg.Wait()
}

func conc1() error {
	const workers, steps = 8, 2000
	clock := ratelimit.NewFakeClock(baseTime())
	l := ratelimit.New(ratelimit.Config{Rate: 1000, Burst: 10, Clock: clock})
	runConcurrent(l, workers, steps)
	st := l.Stats()
	if st.Issued != int64(workers*steps) {
		return fmt.Errorf("期望=Issued=%d（每次调用都计数，不许丢更新） 实际=%d", workers*steps, st.Issued)
	}
	if st.Issued != st.Allowed+st.Denied {
		return fmt.Errorf("期望=Issued==Allowed+Denied（%d） 实际=Allowed=%d Denied=%d", st.Issued, st.Allowed, st.Denied)
	}
	return nil
}

func conc2() error {
	const burst = 50
	clock := ratelimit.NewFakeClock(baseTime())
	l := ratelimit.New(ratelimit.Config{Rate: 0, Burst: burst, Clock: clock})
	runConcurrent(l, 8, 500)
	st := l.Stats()
	left := l.Tokens()
	if sum := float64(st.Allowed) + left; math.Abs(sum-burst) > 1e-9 {
		return fmt.Errorf("期望=Allowed+Tokens()=%d（速率 0 时令牌守恒） 实际=Allowed=%d Tokens()=%v", burst, st.Allowed, left)
	}
	return nil
}

func conc3() error {
	const burst = 50
	clock := ratelimit.NewFakeClock(baseTime())
	l := ratelimit.New(ratelimit.Config{Rate: 0, Burst: burst, Clock: clock})
	runConcurrent(l, 8, 500)
	if left := l.Tokens(); left < 0 || left > burst {
		return fmt.Errorf("期望=并发结束后 Tokens() 落在 [0,%d] 实际=%v", burst, left)
	}
	return nil
}

func clk1() error {
	clock := ratelimit.NewFakeClock(baseTime())
	l := ratelimit.New(ratelimit.Config{Rate: 10, Burst: 10, Clock: clock})
	clock.Advance(-5 * time.Second)
	got := l.Tokens()
	if got < 0 || got > 10 {
		return fmt.Errorf("期望=时钟回退 5 秒后 Tokens() 落在 [0,10] 实际=%v", got)
	}
	return nil
}

func clk2() error {
	clock := ratelimit.NewFakeClock(baseTime())
	l := ratelimit.New(ratelimit.Config{Rate: 2, Burst: 100, Clock: clock})
	for i := 0; i < 10; i++ {
		if !l.Allow() {
			return fmt.Errorf("期望=前 10 次取令牌都成功 实际=第 %d 次被拒绝", i+1)
		}
	}
	clock.Advance(3 * time.Second)
	got := l.Tokens()
	if math.Abs(got-96) > 1e-9 {
		return fmt.Errorf("期望=前进 3 秒后 Tokens()=96 实际=%v", got)
	}
	return nil
}

func deny1() error {
	clock := ratelimit.NewFakeClock(baseTime())
	l := ratelimit.New(ratelimit.Config{Rate: 0, Burst: 3, Clock: clock})
	for i := 0; i < 3; i++ {
		if !l.Allow() {
			return fmt.Errorf("期望=第 %d 次取令牌成功 实际=被拒绝", i+1)
		}
	}
	if l.Allow() {
		return fmt.Errorf("期望=令牌用尽后被拒绝 实际=成功")
	}
	if got := l.Tokens(); got != 0 {
		return fmt.Errorf("期望=被拒绝之后 Tokens()=0 实际=%v", got)
	}
	return nil
}

func deny2() error {
	clock := ratelimit.NewFakeClock(baseTime())
	l := ratelimit.New(ratelimit.Config{Rate: 0, Burst: 2, Clock: clock})
	l.Allow()
	l.Allow()
	for i := 0; i < 100; i++ {
		l.Allow()
	}
	if got := l.Tokens(); got != 0 {
		return fmt.Errorf("期望=连续 100 次拒绝后 Tokens() 仍为 0 实际=%v", got)
	}
	return nil
}

func deny3() error {
	clock := ratelimit.NewFakeClock(baseTime())
	l := ratelimit.New(ratelimit.Config{Rate: 10, Burst: 1, Clock: clock})
	if !l.Allow() {
		return fmt.Errorf("期望=第一次取令牌成功 实际=被拒绝")
	}
	before := l.Stats()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := l.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		return fmt.Errorf("期望=Wait 返回 context.Canceled 实际=%v", err)
	}
	if n := len(clock.Slept()); n != 0 {
		return fmt.Errorf("期望=ctx 已取消时不做任何等待（等待次数 0） 实际=%d", n)
	}
	if after := l.Stats(); after != before {
		return fmt.Errorf("期望=取消后统计不变（%+v） 实际=%+v", before, after)
	}
	return nil
}

func pur1() error {
	for _, f := range []string{"bucket.go", "stats.go"} {
		src, err := readSource(f)
		if err != nil {
			return fmt.Errorf("期望=能读到 %s 实际=%v", f, err)
		}
		code := stripComments(src)
		for _, bad := range []string{"time.Now(", "time.Since(", "time.Sleep("} {
			if strings.Contains(code, bad) {
				return fmt.Errorf("期望=%s 里不出现 %s 实际=出现了", f, bad)
			}
		}
	}

	clock := ratelimit.NewFakeClock(baseTime())
	l := ratelimit.New(ratelimit.Config{Rate: 5, Burst: 5, Clock: clock})
	for i := 0; i < 5; i++ {
		if !l.Allow() {
			return fmt.Errorf("期望=前 5 次取令牌都成功 实际=第 %d 次被拒绝", i+1)
		}
	}
	if l.Allow() {
		return fmt.Errorf("期望=注入时钟不前进时第 6 次被拒绝 实际=成功")
	}
	clock.Advance(time.Second)
	if !l.Allow() {
		return fmt.Errorf("期望=注入时钟前进 1 秒后可再取到一个令牌 实际=被拒绝")
	}
	return nil
}

func pur2() error {
	run := func() (int64, int64, int64) {
		clock := ratelimit.NewFakeClock(baseTime())
		l := ratelimit.New(ratelimit.Config{Rate: 3, Burst: 7, Clock: clock})
		for i := 0; i < 20; i++ {
			l.Allow()
			clock.Advance(100 * time.Millisecond)
		}
		st := l.Stats()
		return st.Issued, st.Allowed, st.Denied
	}
	a1, a2, a3 := run()
	b1, b2, b3 := run()
	if a1 != b1 || a2 != b2 || a3 != b3 {
		return fmt.Errorf("期望=同一调用序列两次结果相同 (%d,%d,%d) 实际=(%d,%d,%d)", a1, a2, a3, b1, b2, b3)
	}
	return nil
}

func readSource(name string) (string, error) {
	for _, p := range []string{name, filepath.Join("..", name)} {
		if b, err := os.ReadFile(p); err == nil {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("找不到源文件 %s（请在仓库根目录运行）", name)
}

// stripComments 去掉行注释，避免注释里提到 time.Now 之类被误判。
func stripComments(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "//"); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	return strings.Join(lines, "\n")
}
