package access

import (
	"sync"
	"time"
)

// maxBuckets acota la memoria: con X-Forwarded-For falseado se podrían crear
// cupos sin fin. Al llenarse, las IP nuevas comparten un cupo común.
const maxBuckets = 4096

// limiter es un balde de fichas por IP: burst fichas que se reponen a rate por segundo.
type limiter struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	now     func() time.Time
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newLimiter(rate float64, burst int, now func() time.Time) *limiter {
	return &limiter{rate: rate, burst: float64(burst), now: now, buckets: map[string]*bucket{}}
}

// get devuelve el balde de key ya repuesto. Se llama con mu tomado.
func (l *limiter) get(key string) *bucket {
	t := l.now()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= maxBuckets {
			l.sweep(t)
		}
		if len(l.buckets) >= maxBuckets {
			key = "*"
			if b, ok = l.buckets[key]; ok {
				l.refill(b, t)
				return b
			}
		}
		b = &bucket{tokens: l.burst, at: t}
		l.buckets[key] = b
		return b
	}
	l.refill(b, t)
	return b
}

func (l *limiter) refill(b *bucket, t time.Time) {
	b.tokens = min(l.burst, b.tokens+t.Sub(b.at).Seconds()*l.rate)
	b.at = t
}

// sweep borra los baldes que ya se llenaron: esas IP no tienen nada pendiente.
func (l *limiter) sweep(t time.Time) {
	for k, b := range l.buckets {
		if b.tokens+t.Sub(b.at).Seconds()*l.rate >= l.burst {
			delete(l.buckets, k)
		}
	}
}

// allow gasta una ficha si hay.
func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.get(key)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// blocked dice si key ya no tiene fichas, sin gastar ninguna.
func (l *limiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.get(key).tokens < 1
}

// consume gasta una ficha (un intento fallido).
func (l *limiter) consume(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b := l.get(key); b.tokens >= 1 {
		b.tokens--
	}
}
