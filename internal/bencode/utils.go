package bencode

import (
	"fmt"
)

func ExtractInfoBytes(data []byte) ([]byte, error) {
	parser := &PacketParser{data: data}

	firstChar := parser.NextChunk(1)
	if firstChar == nil || firstChar[0] != 'd' {
		return nil, fmt.Errorf("missing dic prefix 'd': %w", ErrMalformed)
	}

	for nextChar := parser.Peek(); nextChar != nil && *nextChar != 'e'; nextChar = parser.Peek() {
		keyObj, err := ParseString(parser)
		if err != nil {
			return nil, fmt.Errorf("bad dic key: %w", err)
		}

		startIdx := parser.cursor

		_, err = parseElement(parser)
		if err != nil {
			return nil, fmt.Errorf("bad dic element: %w", err)
		}

		if keyObj.(string) == "info" {
			return data[startIdx:parser.cursor], nil
		}
	}

	lastChar := parser.NextChunk(1)
	if lastChar == nil || lastChar[0] != 'e' {
		return nil, fmt.Errorf("unexpected end of input (not 'e'): %w", ErrMalformed)
	}

	return nil, fmt.Errorf("'info' not found: %w", ErrMalformed)
}
