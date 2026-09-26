// repro 是压测同学留下的复现脚本：并发打同一个令牌桶，看实际放行量
// 有没有超过「桶容量 + 速率 × 已流逝时间」这个上界。
//
//	go run ./repro -rate 100 -burst 10 -workers 8 -steps 2000
//
// 假时钟全程不前进，所以上界就是桶容量本身。
package main

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"ratelimit"
)

func main() {
	rate := flag.Float64("rate", 100, "补充速率（个/秒）")
	burst := flag.Float64("burst", 10, "桶容量")
	workers := flag.Int("workers", 8, "并发 goroutine 数")
	steps := flag.Int("steps", 2000, "每个 goroutine 的请求次数")
	rounds := flag.Int("rounds", 3, "复现轮数")
	flag.Parse()

	calls := (*workers) * (*steps)
	fmt.Printf("配置：rate=%.0f/s burst=%.0f workers=%d steps=%d rounds=%d\n",
		*rate, *burst, *workers, *steps, *rounds)
	fmt.Printf("时钟：假时钟，全程未前进（上界 = burst = %.0f）\n\n", *burst)

	bad := false
	for r := 1; r <= *rounds; r++ {
		clock := ratelimit.NewFakeClock(time.Unix(1700000000, 0))
		l := ratelimit.New(ratelimit.Config{Rate: *rate, Burst: *burst, Clock: clock})

		real := runConcurrent(l, *workers, *steps)
		st := l.Stats()
		ratio := float64(real) / *burst

		fmt.Printf("第 %d 轮：真正放行=%d 上界=%.0f ratio=%.2f\n", r, real, *burst, ratio)
		fmt.Printf("        共享计数：allowed=%d denied=%d issued=%d（总调用 %d）\n",
			st.Allowed, st.Denied, st.Issued, calls)

		if ratio > 1.0 {
			fmt.Printf("        → 放行量超过上界（超发）\n")
			bad = true
		}
		if st.Issued != int64(calls) {
			fmt.Printf("        → 计数异常：issued=%d，但总调用次数是 %d\n", st.Issued, calls)
			bad = true
		}
	}

	fmt.Println()
	if bad {
		fmt.Printf("结论：复现成功，至少一轮不符合「对外保证」第 1 条或第 7 条\n")
		os.Exit(1)
	}
	fmt.Printf("结论：各轮放行量都在上界之内，计数也准确\n")
}

// runConcurrent 让 workers 个 goroutine 在栅栏后同时开始请求，
// 返回真正被放行的次数（各 goroutine 用自己的本地计数器统计，
// 不受共享计数丢更新的影响）。
func runConcurrent(l *ratelimit.Limiter, workers, steps int) int {
	var wg sync.WaitGroup
	entered := make(chan struct{}, workers)
	release := make(chan struct{})
	locals := make([]int, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			entered <- struct{}{}
			<-release
			n := 0
			for j := 0; j < steps; j++ {
				if l.Allow() {
					n++
				}
			}
			locals[idx] = n
		}(i)
	}
	for i := 0; i < workers; i++ {
		<-entered
	}
	close(release)
	wg.Wait()
	total := 0
	for _, v := range locals {
		total += v
	}
	return total
}
