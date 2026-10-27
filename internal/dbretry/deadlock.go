// Package dbretry handles only errors proving the whole transaction rolled back.
package dbretry

import "context"

// Run retries complete, self-contained transaction attempts on MariaDB's 1213
// deadlock victim response. The attempt MUST roll back and release its resources
// before returning. Network errors, unknown COMMIT results and lock timeouts are
// never retried. Every new attempt rechecks the caller's original CAS revision.
func Run[T any](ctx context.Context, attempt func() (T, error)) (T, error) {
	var zero T
	for n := 0; n < 3; n++ {
		if e := ctx.Err(); e != nil {
			return zero, e
		}
		value, e := attempt()
		if e == nil || !isDeadlock(e) || n == 2 {
			return value, e
		}
	}
	panic("unreachable transaction retry")
}
