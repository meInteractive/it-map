package parser

import (
	"crypto/sha256"
	"encoding/binary"
	"time"
)

// spreadDeferredRetry распределяет отложенные GET по job; минимальный срок
// провайдера не сокращается, максимум ограничен. Значение не раскрывает job.
func spreadDeferredRetry(jobID, domain string, delay, limit time.Duration) time.Duration {
	if delay <= 0 || limit <= delay {
		return delay
	}
	spread := delay / 4
	if available := limit - delay; spread > available {
		spread = available
	}
	if spread <= 0 {
		return delay
	}
	sum := sha256.Sum256([]byte(domain + "\x00" + jobID))
	offset := binary.BigEndian.Uint64(sum[:8]) % (uint64(spread) + 1)
	return delay + time.Duration(offset)
}
