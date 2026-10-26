package backup

import (
	"encoding/json"
	"testing"
)

func TestSQLScalarLosslessness(t *testing.T) {
	for _, v := range []any{int64(9007199254740992), []byte{0xff}, true} {
		if _, e := sqlScalar(v, "bigint"); e == nil {
			t.Fatal("unrepresentable scalar silently accepted")
		}
	}
	for _, c := range []struct {
		v          any
		kind, want string
	}{{int64(9007199254740991), "bigint", "9007199254740991"}, {[]byte("  CI 😀 <&>  "), "longtext", `"  CI 😀 <&>  "`}, {nil, "longtext", "null"}, {float64(0.5), "double", "0.5"}} {
		got, e := sqlScalar(c.v, c.kind)
		if e != nil || !json.Valid(got) || string(got) != c.want {
			t.Fatal("SQL scalar lost text/type/value")
		}
	}
}
