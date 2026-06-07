package bencode

import (
	"errors"
	"fmt"
	"strconv"
	"unicode"
)

var ErrMalformed = errors.New("malformed bencode")

type ParseFuncType func(*PacketParser) (any, error)

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
		val, err := getParseFunc(*nextChar)(parser)
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
