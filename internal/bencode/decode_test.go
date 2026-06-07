package bencode

import (
	"errors"
	"testing"
)

// --- Decode ---

func TestDecode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    any
		wantErr bool
	}{
		{"string", "3:foo", "foo", false},
		{"integer", "i42e", 42, false},
		{"list", "l3:fooi1ee", []any{"foo", 1}, false},
		{"dict", "d3:keyi99ee", map[string]any{"key": 99}, false},
		{"empty input", "", nil, true},
		{"malformed", "xyz", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if err != nil {
				if !errors.Is(err, ErrMalformed) {
					t.Errorf("error must wrap ErrMalformed, got: %v", err)
				}
				return
			}
			switch want := tt.want.(type) {
			case string:
				if got.(string) != want {
					t.Errorf("got %q, want %q", got, want)
				}
			case int:
				if got.(int) != want {
					t.Errorf("got %v, want %v", got, want)
				}
			case []any:
				list := got.([]any)
				if len(list) != len(want) {
					t.Errorf("list len = %d, want %d", len(list), len(want))
				}
			case map[string]any:
				dict := got.(map[string]any)
				for k, v := range want {
					if dict[k] != v {
						t.Errorf("dict[%q] = %v, want %v", k, dict[k], v)
					}
				}
			}
		})
	}
}

// --- ParseString ---

func TestParseString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"single-digit length", "3:abc", "abc", false},
		{"multi-digit length", "10:helloworld", "helloworld", false},
		{"zero-length string", "0:", "", false},
		{"single char string", "1:x", "x", false},
		{"missing colon", "3abc", "", true},
		{"length exceeds data", "5:abc", "", true},
		{"non-numeric length", "x:abc", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(tt.input)
			got, err := ParseString(p)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if err != nil {
				if !errors.Is(err, ErrMalformed) {
					t.Errorf("error must wrap ErrMalformed, got: %v", err)
				}
				return
			}
			if got.(string) != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// --- ParseInt ---

func TestParseInt(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"positive integer", "i42e", 42, false},
		{"single digit", "i5e", 5, false},
		{"zero", "i0e", 0, false},
		{"negative integer", "i-7e", -7, false},
		{"large integer", "i1000000e", 1000000, false},
		{"missing prefix i", "42e", -1, true},
		{"missing suffix e", "i42", -1, true},
		{"non-numeric body", "ixe", -1, true},
		{"empty body", "ie", -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(tt.input)
			got, err := ParseInt(p)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if err != nil {
				if !errors.Is(err, ErrMalformed) {
					t.Errorf("error must wrap ErrMalformed, got: %v", err)
				}
				return
			}
			if got.(int) != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

// --- ParseList ---

func TestParseList(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
		wantErr bool
	}{
		{"empty list", "le", 0, false},
		{"list of one string", "l3:fooe", 1, false},
		{"list of two strings", "l3:foo3:bare", 2, false},
		{"list of integers", "li1ei2ei3ee", 3, false},
		{"mixed list", "l3:fooi99ee", 2, false},
		{"nested list", "lli1eee", 1, false},
		{"missing prefix l", "3:fooe", 0, true},
		{"missing suffix e", "l3:foo", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(tt.input)
			got, err := ParseList(p)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if err != nil {
				if !errors.Is(err, ErrMalformed) {
					t.Errorf("error must wrap ErrMalformed, got: %v", err)
				}
				return
			}
			list := got.([]any)
			if len(list) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(list), tt.wantLen)
			}
		})
	}
}

func TestParseListValues(t *testing.T) {
	p := newParser("l3:foo3:bare")
	got, err := ParseList(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list := got.([]any)
	if len(list) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(list))
	}
	if list[0].(string) != "foo" {
		t.Errorf("list[0] = %q, want %q", list[0], "foo")
	}
	if list[1].(string) != "bar" {
		t.Errorf("list[1] = %q, want %q", list[1], "bar")
	}
}

func TestParseListIntValues(t *testing.T) {
	p := newParser("li10ei20ee")
	got, err := ParseList(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list := got.([]any)
	if len(list) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(list))
	}
	if list[0].(int) != 10 {
		t.Errorf("list[0] = %v, want 10", list[0])
	}
	if list[1].(int) != 20 {
		t.Errorf("list[1] = %v, want 20", list[1])
	}
}

// --- ParseDict ---

func TestParseDict(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty dict", "de", false},
		{"single string value", "d3:key3:vale", false},
		{"single integer value", "d3:keyi42ee", false},
		{"multiple keys", "d1:ai1e1:bi2ee", false},
		{"missing prefix d", "3:keye", true},
		{"missing suffix e", "d3:key3:val", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(tt.input)
			got, err := ParseDict(p)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if err != nil {
				if !errors.Is(err, ErrMalformed) {
					t.Errorf("error must wrap ErrMalformed, got: %v", err)
				}
				return
			}
			_ = got
		})
	}
}

func TestParseDictValues(t *testing.T) {
	p := newParser("d3:key3:vale")
	got, err := ParseDict(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dict := got.(map[string]any)
	val, ok := dict["key"]
	if !ok {
		t.Fatal("missing key 'key'")
	}
	if val.(string) != "val" {
		t.Errorf("dict['key'] = %q, want 'val'", val)
	}
}

func TestParseDictIntValue(t *testing.T) {
	p := newParser("d3:keyi42ee")
	got, err := ParseDict(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dict := got.(map[string]any)
	val, ok := dict["key"]
	if !ok {
		t.Fatal("missing key 'key'")
	}
	if val.(int) != 42 {
		t.Errorf("dict['key'] = %v, want 42", val)
	}
}

// --- getParseFunc ---

func TestGetParseFunc(t *testing.T) {
	valid := []byte{'i', 'l', 'd', '0', '9'}
	for _, ch := range valid {
		if getParseFunc(ch) == nil {
			t.Errorf("expected non-nil function for %q", ch)
		}
	}

	invalid := []byte{'x', 'e', 'z', '!'}
	for _, ch := range invalid {
		if getParseFunc(ch) != nil {
			t.Errorf("expected nil function for unknown byte %q", ch)
		}
	}
}
