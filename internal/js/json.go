// Package js reproduces the JavaScript semantics taskthing's data and output
// depend on: objects that keep their key order, JSON.stringify/JSON.parse,
// Number formatting and coercion, and Date's local-time arithmetic. The TS
// implementation wrote its files through these, so a byte-identical port has to
// write through them too.
package js

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A JSON value is one of: nil (null), bool, float64, int64 (a BigInt, written
// bare), string, []any, or *Object.

// Object is a JavaScript object: a string-keyed map that remembers insertion
// order, with JS's enumeration rule that integer-like keys come first, in
// ascending numeric order.
type Object struct {
	keys []string
	m    map[string]any
}

func NewObject() *Object { return &Object{m: map[string]any{}} }

// Obj builds an object from alternating key, value arguments.
func Obj(kv ...any) *Object {
	o := NewObject()
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

func (o *Object) Get(key string) (any, bool) {
	v, ok := o.m[key]
	return v, ok
}

// Value is Get without the presence flag; a missing key reads as nil.
func (o *Object) Value(key string) any { return o.m[key] }

func (o *Object) Has(key string) bool {
	_, ok := o.m[key]
	return ok
}

// Set assigns a key; an existing key keeps its position, as in JS.
func (o *Object) Set(key string, value any) {
	if _, ok := o.m[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.m[key] = value
}

func (o *Object) Delete(key string) {
	if _, ok := o.m[key]; !ok {
		return
	}
	delete(o.m, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i:i], o.keys[i+1:]...)
			break
		}
	}
}

func (o *Object) Len() int { return len(o.keys) }

// Keys enumerates in JS order: array-index keys ascending, then the rest in
// insertion order.
func (o *Object) Keys() []string {
	var ints, strs []string
	for _, k := range o.keys {
		if isArrayIndex(k) {
			ints = append(ints, k)
		} else {
			strs = append(strs, k)
		}
	}
	if len(ints) == 0 {
		return append([]string(nil), o.keys...)
	}
	sort.SliceStable(ints, func(i, j int) bool {
		a, _ := strconv.ParseUint(ints[i], 10, 64)
		b, _ := strconv.ParseUint(ints[j], 10, 64)
		return a < b
	})
	return append(ints, strs...)
}

// Clone is a shallow copy, like `{ ...o }`.
func (o *Object) Clone() *Object {
	c := NewObject()
	for _, k := range o.Keys() {
		c.Set(k, o.m[k])
	}
	return c
}

// Assign copies every key of src onto o, like Object.assign / spread.
func (o *Object) Assign(src *Object) *Object {
	for _, k := range src.Keys() {
		o.Set(k, src.m[k])
	}
	return o
}

func isArrayIndex(k string) bool {
	if k == "" || len(k) > 10 || (len(k) > 1 && k[0] == '0') {
		return false
	}
	for _, c := range k {
		if c < '0' || c > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(k, 10, 64)
	return err == nil && n < math.MaxUint32
}

// ── JSON.stringify ───────────────────────────────────────────────────────────

// Stringify is JSON.stringify(v): compact, JavaScript's escaping (no HTML
// escaping, U+2028/U+2029 left raw), JavaScript's number formatting.
func Stringify(v any) string {
	var b strings.Builder
	writeJSON(&b, v, "", "")
	return b.String()
}

// StringifyIndent is JSON.stringify(v, null, indent).
func StringifyIndent(v any, indent int) string {
	var b strings.Builder
	writeJSON(&b, v, strings.Repeat(" ", indent), "")
	return b.String()
}

func writeJSON(b *strings.Builder, v any, indent, cur string) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			b.WriteString("null")
		} else {
			b.WriteString(NumberToString(x))
		}
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case string:
		b.WriteString(Quote(x))
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		inner := cur + indent
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if indent != "" {
				b.WriteString("\n" + inner)
			}
			writeJSON(b, e, indent, inner)
		}
		if indent != "" {
			b.WriteString("\n" + cur)
		}
		b.WriteByte(']')
	case []string:
		arr := make([]any, len(x))
		for i, s := range x {
			arr[i] = s
		}
		writeJSON(b, arr, indent, cur)
	case *Object:
		keys := x.Keys()
		if len(keys) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteByte('{')
		inner := cur + indent
		first := true
		for _, k := range keys {
			val := x.m[k]
			if _, skip := val.(Undefined); skip {
				continue
			}
			if !first {
				b.WriteByte(',')
			}
			first = false
			if indent != "" {
				b.WriteString("\n" + inner)
			}
			b.WriteString(Quote(k))
			b.WriteByte(':')
			if indent != "" {
				b.WriteByte(' ')
			}
			writeJSON(b, val, indent, inner)
		}
		if indent != "" {
			b.WriteString("\n" + cur)
		}
		b.WriteByte('}')
	case Undefined:
		b.WriteString("null")
	default:
		panic(fmt.Sprintf("js.Stringify: unsupported %T", v))
	}
}

// Undefined is a value JSON.stringify drops from objects (an `undefined` field).
type Undefined struct{}

// Quote is JSON.stringify of a string.
func Quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	b.WriteByte('"')
	return b.String()
}

// ── JSON.parse ───────────────────────────────────────────────────────────────

// Parse is JSON.parse: objects keep key order, numbers become float64.
func Parse(text string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, parseError(text)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, parseError(text)
	}
	return v, nil
}

var identifierRe = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*`)

// parseError approximates JavaScriptCore's JSON.parse messages for the common
// shapes of a broken value.
func parseError(text string) error {
	t := strings.TrimSpace(text)
	switch {
	case t == "":
		return fmt.Errorf("JSON Parse error: Unexpected EOF")
	case identifierRe.MatchString(t):
		word := identifierRe.FindString(t)
		if word != "true" && word != "false" && word != "null" {
			return fmt.Errorf("JSON Parse error: Unexpected identifier %s", Quote(word))
		}
		return fmt.Errorf("JSON Parse error: Unable to parse JSON string")
	case strings.HasPrefix(t, `"`) && strings.Count(t, `"`)%2 == 1:
		return fmt.Errorf("JSON Parse error: Unterminated string")
	case strings.HasPrefix(t, "{") && !strings.HasSuffix(t, "}"):
		return fmt.Errorf("JSON Parse error: Expected '}'")
	case strings.HasPrefix(t, "[") && !strings.HasSuffix(t, "]"):
		return fmt.Errorf("JSON Parse error: Expected ']'")
	}
	var probe any
	if json.Unmarshal([]byte(t), &probe) == nil {
		return fmt.Errorf("JSON Parse error: Unable to parse JSON string")
	}
	return fmt.Errorf("JSON Parse error: Unexpected token")
}

func parseValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := NewObject()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("bad key")
				}
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				o.Set(k, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return o, nil
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("unexpected delimiter")
	case json.Number:
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil {
			return nil, err
		}
		return f, nil
	default:
		return t, nil // string, bool, nil
	}
}

// Equal compares two JSON values by content (as their serializations would).
func Equal(a, b any) bool {
	return bytes.Equal([]byte(Stringify(a)), []byte(Stringify(b)))
}
