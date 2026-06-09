package bencode

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"unicode"
)

var ErrMalformed = errors.New("malformed bencode")

type ParseFuncType func(*PacketParser) (any, error)

type Unmarshaler interface {
	UnmarshalBencode(data any) error
}

func FillDecoded(data any, v any) error {
	return fill(data, v)
}

func Unmarshal(data []byte, v any) error {
	decoded, err := Decode(data)
	if err != nil {
		return err
	}
	return fill(decoded, v)
}

func Decode(data []byte) (any, error) {
	p := &PacketParser{data: data}
	return parseElement(p)
}

func parseElement(parser *PacketParser) (any, error) {
	nextChar := parser.Peek()
	if nextChar == nil {
		return nil, ErrMalformed
	}

	fn := getParseFunc(*nextChar)
	if fn == nil {
		return nil, fmt.Errorf("unknown type byte %q: %w", *nextChar, ErrMalformed)
	}

	return fn(parser)
}

func ParseDict(parser *PacketParser) (any, error) {
	firstChar := parser.NextChunk(1)
	if firstChar == nil || firstChar[0] != 'd' {
		return []any{}, fmt.Errorf("missing dic prefix 'd': %w", ErrMalformed)
	}

	ret := make(map[string]any)
	for nextChar := parser.Peek(); nextChar != nil && unicode.IsDigit(rune(*nextChar)); nextChar = parser.Peek() {
		key_obj, err := ParseString(parser)
		if err != nil {
			return nil, fmt.Errorf("bad dic key: %w", ErrMalformed)
		}

		var element any
		element, err =
			parseElement(parser)
		if err != nil {
			return nil, fmt.Errorf("bad dic element: %w", ErrMalformed)
		}

		ret[key_obj.(string)] = element
	}

	lastChar := parser.NextChunk(1)
	if lastChar == nil || lastChar[0] != 'e' {
		return nil, fmt.Errorf("unexpected end of input (not 'e'): %w", ErrMalformed)
	}

	return ret, nil
}

func ParseList(parser *PacketParser) (any, error) {
	firstChar := parser.NextChunk(1)
	if firstChar == nil || firstChar[0] != 'l' {
		return []any{}, fmt.Errorf("missing list prefix 'l': %w", ErrMalformed)
	}

	var ret []any
	for nextChar := parser.Peek(); nextChar != nil && *nextChar != 'e'; nextChar = parser.Peek() {
		val, err := parseElement(parser)
		if err != nil {
			return nil, err
		}
		ret = append(ret, val)
	}

	lastChar := parser.NextChunk(1)
	if lastChar == nil || lastChar[0] != 'e' {
		return nil, fmt.Errorf("unexpected end of input (not 'e'): %w", ErrMalformed)
	}

	return ret, nil
}

func ParseString(parser *PacketParser) (any, error) {
	lenChunk := parser.ReadUntil(':')
	if lenChunk == nil {
		return "", fmt.Errorf("missing length prefix: %w", ErrMalformed)
	}

	n, err := strconv.Atoi(string(lenChunk))
	if err != nil {
		return "", fmt.Errorf("invalid length %q: %w", lenChunk, ErrMalformed)
	}

	// Confirmed with ReadUntil. Ignore return val
	parser.NextChunk(1)

	strChunk := parser.NextChunk(n)
	if strChunk == nil {
		return "", fmt.Errorf("unexpected end of input: %w", ErrMalformed)
	}

	return string(strChunk), nil
}

func ParseInt(parser *PacketParser) (any, error) {
	firstChar := parser.NextChunk(1)
	if firstChar == nil || firstChar[0] != 'i' {
		return -1, fmt.Errorf("missing integer prefix 'i': %w", ErrMalformed)
	}

	numChunk := parser.ReadUntil('e')
	if numChunk == nil {
		return -1, fmt.Errorf("invalid integer: %w", ErrMalformed)
	}

	num, err := strconv.Atoi(string(numChunk))
	if err != nil {
		return -1, fmt.Errorf("invalid integer %q: %w", numChunk, ErrMalformed)
	}

	lastChar := parser.NextChunk(1)
	if lastChar == nil || lastChar[0] != 'e' {
		return -1, fmt.Errorf("unexpected end of input (not 'e'): %w", ErrMalformed)
	}

	return num, nil
}

func getParseFunc(ch byte) ParseFuncType {
	switch {
	case ch == 'i':
		return ParseInt
	case ch == 'l':
		return ParseList
	case ch == 'd':
		return ParseDict
	case ch >= '0' && ch <= '9':
		return ParseString
	default:
		return nil
	}
}

func fill(data any, dest any) error {
	v := reflect.ValueOf(dest)

	if v.Kind() != reflect.Ptr {
		return fmt.Errorf("dest must be a pointer")
	}

	v = v.Elem()

	if u, ok := dest.(Unmarshaler); ok {
		return u.UnmarshalBencode(data)
	}

	switch v.Kind() {
	case reflect.Int:
		n, ok := data.(int)
		if !ok {
			return fmt.Errorf("expected int, got %T", data)
		}
		v.SetInt(int64(n))
	case reflect.String:
		s, ok := data.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", data)
		}
		v.SetString(s)
	case reflect.Slice:
		// Bencode has no binary type — raw bytes are encoded as strings.
		// []byte destinations must accept a string from Decode instead of []any.
		if v.Type() == reflect.TypeOf([]byte{}) {
			s, ok := data.(string)
			if !ok {
				return fmt.Errorf("expected string for []byte, got %T", data)
			}
			v.Set(reflect.ValueOf([]byte(s)))
			break
		}
		l, ok := data.([]any)
		if !ok {
			return fmt.Errorf("expected list, got %T", data)
		}
		for _, item := range l {
			elem := reflect.New(v.Type().Elem())
			if err := fill(item, elem.Interface()); err != nil {
				return err
			}
			v.Set(reflect.Append(v, elem.Elem()))
		}

	case reflect.Struct:
		m, ok := data.(map[string]any)
		if !ok {
			return fmt.Errorf("expected map, got %T", data)
		}
		for i := range v.NumField() {
			tag := v.Type().Field(i).Tag.Get("bencode")
			field := v.Type().Field(i)
			if tag == "" {
				if field.Anonymous {
					if err := fill(data, v.Field(i).Addr().Interface()); err != nil {
						return err
					}
				}
				continue
			}
			bencodeVal, ok := m[tag]
			if !ok {
				continue
			}
			if err := fill(bencodeVal, v.Field(i).Addr().Interface()); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("Unsupported type")
	}

	return nil
}
