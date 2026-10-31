// Package contracts contains the bounded JSON boundary shared by wire and setup files.
package contracts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid JSON contract")
var ErrTooLarge = errors.New("JSON contract exceeds byte limit")

const MaxDepth = 32

// Parse consumes the entire bounded input, including whitespace after the value.
// It retains numeric precision and rejects duplicate keys before any typed decode.
func Parse(r io.Reader, limit int64) (any, []byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, nil, ErrInvalid
	}
	if int64(len(data)) > limit {
		return nil, nil, ErrTooLarge
	}
	if !utf8.Valid(data) {
		return nil, nil, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := readValue(decoder, 0)
	if err != nil {
		return nil, nil, ErrInvalid
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, nil, ErrInvalid
	}
	return value, data, nil
}

func readValue(d *json.Decoder, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, ErrInvalid
	}
	token, err := d.Token()
	if err != nil {
		return nil, ErrInvalid
	}
	switch token {
	case json.Delim('{'):
		object := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, ErrInvalid
			}
			name, ok := key.(string)
			if !ok {
				return nil, ErrInvalid
			}
			if _, exists := object[name]; exists {
				return nil, ErrInvalid
			}
			value, err := readValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			object[name] = value
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return nil, ErrInvalid
		}
		return object, nil
	case json.Delim('['):
		array := []any{}
		for d.More() {
			value, err := readValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return nil, ErrInvalid
		}
		return array, nil
	default:
		// This contract set has only bounded integer numeric fields. Reject floating
		// syntax/exponents before the validator can allocate an enormous rational.
		if number, ok := token.(json.Number); ok && (len(number.String()) > 17 || strings.ContainsAny(number.String(), ".eE")) {
			return nil, ErrInvalid
		}
		if _, isDelimiter := token.(json.Delim); isDelimiter {
			return nil, ErrInvalid
		}
		return token, nil
	}
}
