package jsontotoml

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

// decodeJSON decodes a JSON document with json.Number semantics so that the
// integer-vs-float distinction is preserved, matching serde_json.
func decodeJSON(t *testing.T, doc string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(doc))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode %q: %v", doc, err)
	}
	return v
}

func TestJSONToTOML_Conversion(t *testing.T) {
	tests := []struct {
		name string
		json string
		want Value
	}{
		{
			name: "integer",
			json: `123`,
			want: IntValue(123),
		},
		{
			name: "negative integer",
			json: `-7`,
			want: IntValue(-7),
		},
		{
			name: "array of mixed scalars",
			json: `[true, 1]`,
			want: ArrayValue([]Value{BoolValue(true), IntValue(1)}),
		},
		{
			name: "bool false",
			json: `false`,
			want: BoolValue(false),
		},
		{
			name: "bool true",
			json: `true`,
			want: BoolValue(true),
		},
		{
			name: "float",
			json: `1.25`,
			want: FloatValue(1.25),
		},
		{
			name: "null becomes empty string",
			json: `null`,
			want: StringValue(""),
		},
		{
			name: "string",
			json: `"hello"`,
			want: StringValue("hello"),
		},
		{
			name: "nested object",
			json: `{ "outer": { "inner": 2 } }`,
			want: TableValue([]TableEntry{{
				Key: "outer",
				Value: TableValue([]TableEntry{{
					Key:   "inner",
					Value: IntValue(2),
				}}),
			}}),
		},
		{
			name: "object keys sorted",
			json: `{ "b": 2, "a": 1 }`,
			want: TableValue([]TableEntry{
				{Key: "a", Value: IntValue(1)},
				{Key: "b", Value: IntValue(2)},
			}),
		},
		{
			name: "empty array",
			json: `[]`,
			want: ArrayValue(nil),
		},
		{
			name: "empty object",
			json: `{}`,
			want: TableValue(nil),
		},
		{
			name: "large integer beyond int64 falls back to float or string",
			json: `100000000000000000000`,
			// 1e20 is finite and representable as float64, so it becomes a float.
			want: FloatValue(1e20),
		},
		{
			name: "fractional that is whole stays float via decoder",
			json: `2.0`,
			want: FloatValue(2.0),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := JSONToTOML(decodeJSON(t, tc.json))
			if !got.Equal(tc.want) {
				t.Fatalf("JSONToTOML(%s) = %+v, want %+v", tc.json, got, tc.want)
			}
		})
	}
}

func TestJSONToTOML_NativeTypes(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  Value
	}{
		{name: "nil", input: nil, want: StringValue("")},
		{name: "go bool", input: true, want: BoolValue(true)},
		{name: "go int", input: 42, want: IntValue(42)},
		{name: "go int64", input: int64(-9), want: IntValue(-9)},
		{name: "go uint", input: uint(5), want: IntValue(5)},
		{name: "go float64 whole becomes int", input: float64(3), want: IntValue(3)},
		{name: "go float64 fractional", input: float64(2.5), want: FloatValue(2.5)},
		{name: "go string", input: "x", want: StringValue("x")},
		{
			name:  "go slice",
			input: []any{1, "a"},
			want:  ArrayValue([]Value{IntValue(1), StringValue("a")}),
		},
		{
			name:  "go map sorted",
			input: map[string]any{"z": 1, "a": 2},
			want: TableValue([]TableEntry{
				{Key: "a", Value: IntValue(2)},
				{Key: "z", Value: IntValue(1)},
			}),
		},
		{
			name:  "uint64 overflow becomes string",
			input: uint64(1) << 63,
			want:  StringValue("9223372036854775808"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := JSONToTOML(tc.input)
			if !got.Equal(tc.want) {
				t.Fatalf("JSONToTOML(%v) = %+v, want %+v", tc.input, got, tc.want)
			}
		})
	}
}

func TestJSONToTOML_RawMessage(t *testing.T) {
	raw := json.RawMessage(`{"n": 9, "s": "hi"}`)
	got := JSONToTOML(raw)
	want := TableValue([]TableEntry{
		{Key: "n", Value: IntValue(9)},
		{Key: "s", Value: StringValue("hi")},
	})
	if !got.Equal(want) {
		t.Fatalf("JSONToTOML(raw) = %+v, want %+v", got, want)
	}
}

func TestEncode_Table(t *testing.T) {
	tests := []struct {
		name string
		in   Value
		want string
	}{
		{
			name: "scalars",
			in: TableValue([]TableEntry{
				{Key: "name", Value: StringValue("codex")},
				{Key: "count", Value: IntValue(3)},
				{Key: "ratio", Value: FloatValue(1.5)},
				{Key: "on", Value: BoolValue(true)},
			}),
			want: "name = \"codex\"\ncount = 3\nratio = 1.5\non = true\n",
		},
		{
			name: "array value",
			in: TableValue([]TableEntry{
				{Key: "items", Value: ArrayValue([]Value{IntValue(1), IntValue(2)})},
			}),
			want: "items = [1, 2]\n",
		},
		{
			name: "nested table header",
			in: TableValue([]TableEntry{
				{Key: "a", Value: IntValue(1)},
				{Key: "outer", Value: TableValue([]TableEntry{
					{Key: "inner", Value: IntValue(2)},
				})},
			}),
			want: "a = 1\n\n[outer]\ninner = 2\n",
		},
		{
			name: "quoted key and string escaping",
			in: TableValue([]TableEntry{
				{Key: "needs quote", Value: StringValue("line\nbreak")},
			}),
			want: "\"needs quote\" = \"line\\nbreak\"\n",
		},
		{
			name: "whole float keeps .0",
			in: TableValue([]TableEntry{
				{Key: "f", Value: FloatValue(2)},
			}),
			want: "f = 2.0\n",
		},
		{
			name: "inline table in array",
			in: TableValue([]TableEntry{
				{Key: "rows", Value: ArrayValue([]Value{
					TableValue([]TableEntry{{Key: "x", Value: IntValue(1)}}),
				})},
			}),
			want: "rows = [{ x = 1 }]\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.in.Encode()
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Encode() =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestEncode_NonTableRoot(t *testing.T) {
	cases := []Value{
		StringValue("x"),
		IntValue(1),
		BoolValue(true),
		FloatValue(1.5),
		ArrayValue([]Value{IntValue(1)}),
	}
	for _, c := range cases {
		if _, err := c.Encode(); !errors.Is(err, ErrTopLevelNotTable) {
			t.Fatalf("Encode() error = %v, want ErrTopLevelNotTable for kind %d", err, c.Kind())
		}
	}
}

func TestValue_AccessorsCopy(t *testing.T) {
	arr := ArrayValue([]Value{IntValue(1), IntValue(2)})
	got := arr.Array()
	got[0] = IntValue(99)
	// Original must be unchanged: accessors return copies (immutability).
	again := arr.Array()
	if !again[0].Equal(IntValue(1)) {
		t.Fatalf("Array() did not return an independent copy: %+v", again)
	}

	tbl := TableValue([]TableEntry{{Key: "k", Value: IntValue(1)}})
	entries := tbl.Table()
	entries[0].Value = IntValue(99)
	againTbl := tbl.Table()
	if !againTbl[0].Value.Equal(IntValue(1)) {
		t.Fatalf("Table() did not return an independent copy: %+v", againTbl)
	}
}

func TestNumberToTOML_StringFallback(t *testing.T) {
	// A json.Number token that is neither a valid int64 nor a finite float
	// falls back to the original string token, matching serde_json.
	got := JSONToTOML(json.Number("1e999"))
	if !got.Equal(StringValue("1e999")) {
		t.Fatalf("numberToTOML overflow = %+v, want string fallback", got)
	}

	// A non-integer token still becomes a float.
	gotFloat := JSONToTOML(json.Number("3.5"))
	if !gotFloat.Equal(FloatValue(3.5)) {
		t.Fatalf("numberToTOML(3.5) = %+v, want FloatValue(3.5)", gotFloat)
	}
}

func TestJSONToTOML_RawMapAndSliceTypes(t *testing.T) {
	rawMap := map[string]json.RawMessage{
		"b": json.RawMessage(`2`),
		"a": json.RawMessage(`"x"`),
	}
	got := JSONToTOML(rawMap)
	want := TableValue([]TableEntry{
		{Key: "a", Value: StringValue("x")},
		{Key: "b", Value: IntValue(2)},
	})
	if !got.Equal(want) {
		t.Fatalf("rawMap = %+v, want %+v", got, want)
	}

	rawSlice := []json.RawMessage{json.RawMessage(`1`), json.RawMessage(`true`)}
	gotSlice := JSONToTOML(rawSlice)
	wantSlice := ArrayValue([]Value{IntValue(1), BoolValue(true)})
	if !gotSlice.Equal(wantSlice) {
		t.Fatalf("rawSlice = %+v, want %+v", gotSlice, wantSlice)
	}
}

func TestJSONToTOML_UnknownTypeStringified(t *testing.T) {
	type custom struct {
		A int `json:"a"`
	}
	got := JSONToTOML(custom{A: 1})
	if got.Kind() != KindString || got.String() != `{"a":1}` {
		t.Fatalf("unknown type = %+v, want StringValue(%q)", got, `{"a":1}`)
	}
}

func TestValue_Accessors(t *testing.T) {
	if v := StringValue("s"); v.Kind() != KindString || v.String() != "s" {
		t.Fatalf("StringValue accessors wrong: %+v", v)
	}
	if v := BoolValue(true); v.Kind() != KindBoolean || v.Bool() != true {
		t.Fatalf("BoolValue accessors wrong: %+v", v)
	}
	if v := IntValue(7); v.Kind() != KindInteger || v.Int() != 7 {
		t.Fatalf("IntValue accessors wrong: %+v", v)
	}
	if v := FloatValue(1.5); v.Kind() != KindFloat || v.Float() != 1.5 {
		t.Fatalf("FloatValue accessors wrong: %+v", v)
	}
}

func TestEncode_NonFiniteFloat(t *testing.T) {
	tests := []struct {
		name string
		val  Value
		want string
	}{
		{name: "positive inf", val: FloatValue(math.Inf(1)), want: "f = inf\n"},
		{name: "negative inf", val: FloatValue(math.Inf(-1)), want: "f = -inf\n"},
		{name: "nan", val: FloatValue(math.NaN()), want: "f = nan\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := TableValue([]TableEntry{{Key: "f", Value: tc.val}})
			got, err := doc.Encode()
			if err != nil {
				t.Fatalf("Encode() error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Encode() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEncode_StringEscapesControlChars(t *testing.T) {
	doc := TableValue([]TableEntry{
		{Key: "k", Value: StringValue("a\tb\rcd")},
	})
	got, err := doc.Encode()
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	want := "k = \"a\\tb\\rc\\u0001d\"\n"
	if got != want {
		t.Fatalf("Encode() = %q, want %q", got, want)
	}
}

func TestRoundTripJSONToTOMLDocument(t *testing.T) {
	const doc = `{"model": "gpt", "nested": {"k": 1}, "list": [1, "two", false]}`
	v := JSONToTOML(decodeJSON(t, doc))
	out, err := v.Encode()
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	// Keys are sorted: list, model, nested. nested is emitted last as a header.
	want := "list = [1, \"two\", false]\nmodel = \"gpt\"\n\n[nested]\nk = 1\n"
	if out != want {
		t.Fatalf("Encode() =\n%q\nwant\n%q", out, want)
	}
}
