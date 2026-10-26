package js

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// NumberToString is JavaScript's Number#toString (ECMAScript Number::toString).
func NumberToString(x float64) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case math.IsInf(x, 1):
		return "Infinity"
	case math.IsInf(x, -1):
		return "-Infinity"
	case x == 0:
		return "0"
	}
	if x < 0 {
		return "-" + NumberToString(-x)
	}
	// Shortest round-tripping digits and the decimal exponent, as d.ddde±n.
	e := strconv.FormatFloat(x, 'e', -1, 64)
	mant, expPart, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	exp, _ := strconv.Atoi(expPart)
	k := len(digits)
	n := exp + 1
	switch {
	case k <= n && n <= 21:
		return digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return "0." + strings.Repeat("0", -n) + digits
	}
	sign := "+"
	if n-1 < 0 {
		sign = "-"
	}
	abs := n - 1
	if abs < 0 {
		abs = -abs
	}
	if k == 1 {
		return digits + "e" + sign + strconv.Itoa(abs)
	}
	return digits[:1] + "." + digits[1:] + "e" + sign + strconv.Itoa(abs)
}

var decimalLiteral = regexp.MustCompile(`^[+-]?(?:(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?|Infinity)$`)

// jsSpace is what String.prototype.trim (and Number()) strip.
const jsSpace = " \t\n\v\f\r\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

// Trim is String.prototype.trim.
func Trim(s string) string { return strings.Trim(s, jsSpace) }

// TrimStart is String.prototype.trimStart.
func TrimStart(s string) string { return strings.TrimLeft(s, jsSpace) }

// ToNumber is Number(s) for a string.
func ToNumber(s string) float64 {
	t := Trim(s)
	if t == "" {
		return 0
	}
	if len(t) > 2 && t[0] == '0' {
		base := 0
		switch t[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			n, err := strconv.ParseUint(t[2:], base, 64)
			if err != nil || strings.ContainsAny(t[2:], "_+-") {
				return math.NaN()
			}
			return float64(n)
		}
	}
	if !decimalLiteral.MatchString(t) {
		return math.NaN()
	}
	switch t {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		// Out of range parses to ±Inf or 0 in JS; ParseFloat reports it.
		return f
	}
	return f
}

// IsInteger is Number.isInteger.
func IsInteger(x float64) bool {
	return !math.IsNaN(x) && !math.IsInf(x, 0) && math.Trunc(x) == x
}

var leadingInt = regexp.MustCompile(`^[+-]?\d+`)

// ParseInt is parseInt(s) (radix 10, which is all taskthing ever asks for):
// leading digits after optional whitespace and sign, else NaN.
func ParseInt(s string) float64 {
	t := TrimStart(s)
	if len(t) > 1 && t[0] == '0' && (t[1] == 'x' || t[1] == 'X') {
		n, err := strconv.ParseInt(hexPrefix(t[2:]), 16, 64)
		if err != nil {
			return math.NaN()
		}
		return float64(n)
	}
	m := leadingInt.FindString(t)
	if m == "" {
		return math.NaN()
	}
	f, _ := strconv.ParseFloat(m, 64)
	return f
}

func hexPrefix(s string) string {
	i := 0
	for i < len(s) && strings.ContainsRune("0123456789abcdefABCDEF", rune(s[i])) {
		i++
	}
	return s[:i]
}

var leadingFloat = regexp.MustCompile(`^[+-]?(?:Infinity|(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?)`)

// ParseFloat is parseFloat(s).
func ParseFloat(s string) float64 {
	m := leadingFloat.FindString(TrimStart(s))
	if m == "" {
		return math.NaN()
	}
	return ToNumber(m)
}
