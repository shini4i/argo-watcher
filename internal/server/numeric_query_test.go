package server

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// strconv.ParseFloat accepts "NaN" and "Inf" with a nil error, and every
// comparison against NaN is false — so a window clamp written as `if start <
// earliest` never fires and the value reaches the query untouched. A finite value
// past the epoch range escapes the same way, into an undefined int64 conversion.
func TestParseFloatQueryRejectsValuesNoClampCanBound(t *testing.T) {
	for _, raw := range []string{
		"NaN", "nan", "Inf", "+Inf", "-Inf", "infinity", "-infinity",
		"1e308", "-1e308", "1e13",
	} {
		t.Run(raw, func(t *testing.T) {
			query := url.Values{"from_timestamp": []string{raw}}
			assert.Equal(t, float64(0), parseFloatQuery(query, "from_timestamp"),
				"a value no clamp can bound must be treated as absent")
		})
	}
}

func TestParseFloatQueryKeepsOrdinaryNumbers(t *testing.T) {
	tests := map[string]float64{
		"0":          0,
		"1700000000": 1700000000,
		"1e12":       1e12,
		"-5":         -5,
		"1.5":        1.5,
		"":           0,
		"not-a-time": 0,
	}

	for raw, want := range tests {
		t.Run(raw, func(t *testing.T) {
			query := url.Values{"from_timestamp": []string{raw}}
			assert.Equal(t, want, parseFloatQuery(query, "from_timestamp"))
		})
	}
}
