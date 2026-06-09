package bencode

import (
	"bytes"
	"errors"
	"testing"
)

func TestExtractInfoBytes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "returns encoded info dictionary",
			input: "d8:announce7:tracker4:infod4:name4:test6:lengthi123eee",
			want:  "d4:name4:test6:lengthi123ee",
		},
		{
			name:  "ignores nested info key",
			input: "d5:otherd4:info3:fooe4:infod4:name4:testee",
			want:  "d4:name4:teste",
		},
		{
			name:  "skips list before info",
			input: "d5:otherl3:fooi2ee4:infod3:key5:valueee",
			want:  "d3:key5:valuee",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractInfoBytes([]byte(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(got, []byte(tt.want)) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractInfoBytesMalformed(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"not top-level dict", "l4:infoe"},
		{"well-formed dict missing info", "d8:announce7:trackere"},
		{"nested info only", "d5:otherd4:info3:fooee"},
		{"malformed top-level key", "d8:announce7:trackerx"},
		{"malformed skipped list value", "d3:foolxe4:infodee"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ExtractInfoBytes([]byte(tt.input))
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrMalformed) {
				t.Errorf("error must wrap ErrMalformed, got: %v", err)
			}
		})
	}
}
