//go:build !darwin && !linux

package materials

import (
	"context"
	"errors"
)

type Content struct{}

func Open(string) (*Content, error) { return nil, ErrUnsupported }
func (*Content) Close() error       { return nil }
func (*Content) Put(context.Context, string, []byte) error {
	return errors.New("material platform unsupported")
}
func (*Content) Read(context.Context, string) ([]byte, string, error) { return nil, "unsupported", nil }
