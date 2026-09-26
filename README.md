# ratelimit

Go 1.24 的令牌桶限流器，只用标准库，不引入任何第三方依赖。

## 怎么跑

```bash
go test ./...                    # 既有用例
go run ./check                   # 固定验收程序（10 个场景）
go run ./check -list             # 列出全部场景
go run ./check -only clock       # 只跑某个分组
go run ./repro                   # 压测同学留下的复现脚本
bash scripts/check.sh            # 等价于 go run ./check
```

`check/` 与 `repro/` 是固定验收的一部分，**请勿修改**。

## 对外保证

1. 任一时刻**已放行总量 ≤ burst + rate × 已流逝时间**，已流逝时间按注入时钟计算，
   允许误差为 0。
2. 时间只能通过注入的 `Clock` 读取。`bucket.go` 与 `stats.go` 里**不得**出现
   `time.Now` / `time.Since` / `time.Sleep`，等待一律走 `Clock.Sleep`；
   `clock.go` 是时钟适配器，不受此限。
3. 并发调用 `Allow()` 安全：N 个 goroutine 在栅栏后同时请求时，
   总放行数不得超过上界，也不得出现重复放行。
4. 时钟**回退**（`FakeClock.Advance` 传负数）不得让桶溢出 ——
   令牌数始终落在 `[0, burst]` 内，既不能超过容量，也不能变成负数。
5. 令牌不足时 `Allow()` 返回 false 且**不消耗**任何令牌，不阻塞。
6. `Wait(ctx)` 在 ctx 取消时返回 `ctx.Err()`，且**不消耗**任何令牌、**不做**任何等待。
7. `Stats()` 在并发下自洽：`Issued == Allowed + Denied` 在**任意瞬间**都成立，
   且 `Issued` 单调不减。`Issued` 统计 `Allow` 的调用次数，含被拒绝的那些。
8. 不依赖墙钟、不依赖 `math/rand` 全局随机源；同一调用序列在任何机器上产生同样的结果。
9. 不访问网络与磁盘。

## 公开接口

```go
type Config struct {
    Rate  float64 // 每秒补充的令牌数
    Burst float64 // 桶容量，新建的桶是满的
    Clock Clock   // 时间来源，nil 表示真实时钟
}

func New(cfg Config) *Limiter
func (l *Limiter) Allow() bool
func (l *Limiter) Wait(ctx context.Context) error
func (l *Limiter) Tokens() float64   // 只读，返回当前可用令牌数，落在 [0, burst]
func (l *Limiter) Stats() Stats
```

`Clock` 由 `clock.go` 提供（接口 + `FakeClock`），已经实现好，可以直接用。

## 目录

```
bucket.go           限流器实现
stats.go            累计统计
clock.go            时钟接口与 FakeClock（已实现）
errors.go           哨兵错误
ratelimit_test.go   既有用例
repro/main.go       压测复现脚本（勿改）
check/main.go       固定验收程序（勿改）
scripts/check.sh    固定验收入口（勿改）
```

## 固定件场景（10 个）

| 分组 | 个数 | 关注点 |
| --- | --- | --- |
| `concurrency` | 3 | 并发下计数准确（不丢更新）、令牌守恒、令牌数取值区间 |
| `clock` | 2 | 时钟回退后的取值区间、按流逝时间成比例补充 |
| `deny` | 3 | 拒绝不消耗令牌、连续拒绝后令牌数、取消后不等待不消耗 |
| `purity` | 2 | 不使用墙钟（静态 + 行为）、同一序列可重复 |

并发场景一律用栅栏（凑齐 8 个 goroutine 才放行），配合 `-count` 多轮重复，
不依赖墙钟快慢。分配与计数类判据与机器速度无关。
