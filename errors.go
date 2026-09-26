package ratelimit

import "errors"

// ErrNotAvailable 表示当前没有可用令牌，且调用方无法等待。
var ErrNotAvailable = errors.New("ratelimit: no token available")
