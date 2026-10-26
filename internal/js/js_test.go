package js

import "testing"

// Expected strings are what Bun's String(number) printed.
func TestNumberToString(t *testing.T) {
	for _, c := range []struct {
		x    float64
		want string
	}{
		{0.0, "0"},
		{1.0, "1"},
		{1.5, "1.5"},
		{1e+21, "1e+21"},
		{1e-07, "1e-7"},
		{123456789012.0, "123456789012"},
		{0.1, "0.1"},
		{1784000000000.0, "1784000000000"},
		{2.5e-07, "2.5e-7"},
		{1e+300, "1e+300"},
		{-1.5e-10, "-1.5e-10"},
		{100.0, "100"},
		{1e+20, "100000000000000000000"},
		{123.456, "123.456"},
		{5e-324, "5e-324"},
		{1e-06, "0.000001"},
		{1.7976931348623157e+308, "1.7976931348623157e+308"},
	} {
		if got := NumberToString(c.x); got != c.want {
			t.Errorf("NumberToString(%v) = %q, want %q", c.x, got, c.want)
		}
	}
}

func TestQuoteAndKeys(t *testing.T) {
	if got := Quote("<b>&\u2028\x01\"\\"); got != "\"<b>&\u2028\\u0001\\\"\\\\\"" {
		t.Errorf("Quote: %s", got)
	}
	o := Obj("b", 1.0, "2", 2.0, "a", 3.0, "1", 4.0)
	if got := Stringify(o); got != `{"1":4,"2":2,"b":1,"a":3}` {
		t.Errorf("integer keys enumerate first: %s", got)
	}
}
