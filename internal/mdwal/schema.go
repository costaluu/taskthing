package mdwal

import (
	"math"
	"strings"

	"github.com/costaluu/taskthing/internal/js"
	"github.com/costaluu/taskthing/internal/rrule"
)

// ── schemas (zod, as schema.ts declares them) ────────────────────────────────
//
// The schema is the only source of truth for field types: it validates what is
// read and written, strips what it does not declare, and orders its output by
// declaration. A failure is a ZodError whose message is the issue list as zod
// v4 prints it, since that text reaches the user verbatim.

// Field validates one value, appending its issues at path.
type Field struct {
	Name       string
	Kind       string // string, boolean, number, enum, rrule
	NonEmpty   bool
	Nullable   bool
	Int        bool
	Positive   bool
	Values     []string // enum
	Default    any      // nil with HasDefault means a null default
	HasDefault bool
}

// Schema is a zod object schema over ordered fields.
type Schema struct{ Fields []Field }

// Omit is schema.omit({ name: true }).
func (s *Schema) Omit(name string) *Schema {
	out := &Schema{}
	for _, f := range s.Fields {
		if f.Name != name {
			out.Fields = append(out.Fields, f)
		}
	}
	return out
}

// ZodError is a failed parse.
type ZodError struct{ Issues []any }

func (e *ZodError) Error() string { return js.StringifyIndent(e.Issues, 2) }

// Parse is schema.parse: the declared fields, defaults applied, in order.
func (s *Schema) Parse(input *js.Object) (*js.Object, error) {
	out := js.NewObject()
	var issues []any
	for _, f := range s.Fields {
		v, ok := input.Get(f.Name)
		if _, undef := v.(js.Undefined); undef {
			ok = false
		}
		if !ok && f.HasDefault {
			out.Set(f.Name, f.Default)
			continue
		}
		var value any = js.Undefined{}
		if ok {
			value = v
		}
		if found := f.check(value); len(found) > 0 {
			issues = append(issues, found...)
			continue
		}
		if ok {
			out.Set(f.Name, v)
		}
	}
	if len(issues) > 0 {
		return nil, &ZodError{Issues: issues}
	}
	return out, nil
}

func path(name string) []any { return []any{name} }

// typeName is how zod names a received value.
func typeName(v any) string {
	switch x := v.(type) {
	case js.Undefined:
		return "undefined"
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		if math.IsNaN(x) {
			return "NaN"
		}
		return "number"
	case int64:
		return "bigint"
	case []any:
		return "array"
	}
	return "object"
}

func invalidType(f Field, expected string, v any) []any {
	return []any{js.Obj(
		"expected", expected,
		"code", "invalid_type",
		"path", path(f.Name),
		"message", "Invalid input: expected "+expected+", received "+typeName(v),
	)}
}

func (f Field) check(v any) []any {
	if v == nil && f.Nullable {
		return nil
	}
	switch f.Kind {
	case "string", "rrule":
		s, ok := v.(string)
		if !ok {
			return invalidType(f, "string", v)
		}
		if f.NonEmpty && len(s) == 0 {
			return []any{js.Obj(
				"origin", "string",
				"code", "too_small",
				"minimum", 1.0,
				"inclusive", true,
				"path", path(f.Name),
				"message", "Too small: expected string to have >=1 characters",
			)}
		}
		if f.Kind == "rrule" && !rrule.Valid(s) {
			return []any{js.Obj("code", "custom", "path", path(f.Name), "message", "not a valid recurrence rule")}
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return invalidType(f, "boolean", v)
		}
	case "number":
		n, ok := v.(float64)
		if !ok {
			return invalidType(f, "number", v)
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			received := "NaN"
			name := "NaN"
			if math.IsInf(n, 1) {
				received, name = "Infinity", "number"
			} else if math.IsInf(n, -1) {
				received, name = "-Infinity", "number"
			}
			return []any{js.Obj(
				"expected", "number",
				"code", "invalid_type",
				"received", received,
				"path", path(f.Name),
				"message", "Invalid input: expected number, received "+name,
			)}
		}
		var issues []any
		if f.Int && (!js.IsInteger(n) || math.Abs(n) > 9007199254740991) {
			issues = append(issues, js.Obj(
				"expected", "int",
				"format", "safeint",
				"code", "invalid_type",
				"path", path(f.Name),
				"message", "Invalid input: expected int, received number",
			))
			return issues
		}
		if f.Positive && !(n > 0) {
			issues = append(issues, js.Obj(
				"origin", "number",
				"code", "too_small",
				"minimum", 0.0,
				"inclusive", false,
				"path", path(f.Name),
				"message", "Too small: expected number to be >0",
			))
		}
		return issues
	case "enum":
		s, ok := v.(string)
		for _, allowed := range f.Values {
			if ok && s == allowed {
				return nil
			}
		}
		quoted := make([]string, len(f.Values))
		values := make([]any, len(f.Values))
		for i, a := range f.Values {
			quoted[i] = js.Quote(a)
			values[i] = a
		}
		return []any{js.Obj(
			"code", "invalid_value",
			"values", values,
			"path", path(f.Name),
			"message", "Invalid option: expected one of "+strings.Join(quoted, "|"),
		)}
	}
	return nil
}

// ── the domain schemas (schema.ts) ───────────────────────────────────────────

// INBOX is the virtual board every task belongs to when it has none (ADR-0004).
const INBOX = "inbox"

var TaskSchema = &Schema{Fields: []Field{
	{Name: "id", Kind: "string", NonEmpty: true},
	{Name: "board", Kind: "string", NonEmpty: true, Default: INBOX, HasDefault: true},
	{Name: "completed", Kind: "boolean", Default: false, HasDefault: true},
	{Name: "title", Kind: "string", NonEmpty: true},
	{Name: "star", Kind: "boolean", Default: false, HasDefault: true},
	{Name: "rrule", Kind: "rrule", Nullable: true, Default: nil, HasDefault: true},
	{Name: "description", Kind: "string", Nullable: true, Default: nil, HasDefault: true},
	{Name: "deleted", Kind: "boolean", Default: false, HasDefault: true},
}}

var BoardSchema = &Schema{Fields: []Field{
	{Name: "id", Kind: "string", NonEmpty: true},
	{Name: "name", Kind: "string", NonEmpty: true},
	{Name: "icon", Kind: "string", Default: "", HasDefault: true},
	{Name: "color", Kind: "string", Default: "", HasDefault: true},
	{Name: "deleted", Kind: "boolean", Default: false, HasDefault: true},
}}

var ConfigSchema = &Schema{Fields: []Field{
	{Name: "currentWorkspace", Kind: "string", NonEmpty: true},
	{Name: "dateFormat", Kind: "enum", Values: []string{"america", "europe"}, Default: "america", HasDefault: true},
	{Name: "nerdfont", Kind: "boolean", Default: false, HasDefault: true},
	{Name: "theme", Kind: "string", Default: "", HasDefault: true},
	{Name: "lastUpdateCheck", Kind: "number", Nullable: true, Default: nil, HasDefault: true},
	{Name: "updateAvailable", Kind: "boolean", Default: false, HasDefault: true},
	{Name: "updateTarget", Kind: "string", Nullable: true, Default: nil, HasDefault: true},
	{Name: "autoUpdate", Kind: "enum", Values: []string{"confirm", "silent"}, Default: "confirm", HasDefault: true},
	{Name: "autoSync", Kind: "number", Int: true, Positive: true, Default: 4.0, HasDefault: true},
	{Name: "autoRebuild", Kind: "number", Int: true, Positive: true, Default: 5.0, HasDefault: true},
	{Name: "autoTruncate", Kind: "number", Int: true, Positive: true, Default: 10.0, HasDefault: true},
}}

// The schemas as mdwal stores them: the id names the file, not a field.
var (
	TaskFields  = TaskSchema.Omit("id")
	BoardFields = BoardSchema.Omit("id")
)
