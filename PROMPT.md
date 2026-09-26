压测同学说并发一上来账目就对不上，`go run ./repro` 是他的复现脚本（别改）。
ratelimit 是 Go 1.24 的令牌桶限流器，只用标准库，构建与自检走 `go test` 与
`go run ./check`（`check` 也是固定验收程序，别改）。

README「对外保证」一节列了 9 条；`ratelimit_test.go` 里 13 个用例当前全绿，
但它们只在单 goroutine、时间只前进的前提下跑过。

任务：把 9 条补齐，让既有用例与固定件全过。

验收：
- go test ./... 退出码 0；
- go run ./check 退出码 0，10 个场景全过（concurrency 3 + clock 2 + deny 3 + purity 2）。

约束：
1. 不改 `check/`、不改 `repro/`、不改既有用例的断言；已公开的函数签名不变（可新增）。
2. 不引入第三方依赖，`go.mod` 里不许出现 require。
3. 时间只能通过注入的 `Clock` 读，实现里不许直接调 `time.Now` / `time.Since` / `time.Sleep`。
4. 并发判据用栅栏 + 多轮重复，不依赖墙钟快慢。
