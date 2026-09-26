package ratelimit

// Stats 是限流器的累计统计。
//
// 契约：Issued == Allowed + Denied 在任意瞬间都成立，且 Issued 单调不减。
// Issued 统计的是 Allow 的调用次数，含被拒绝的那些。
type Stats struct {
	Issued  int64
	Allowed int64
	Denied  int64
}

// Stats 返回当前统计快照。
func (l *Limiter) Stats() Stats {
	return Stats{
		Issued:  l.issued,
		Allowed: l.allowed,
		Denied:  l.denied,
	}
}
