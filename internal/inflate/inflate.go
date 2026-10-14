// Package inflate decompresses zlib data with a cap on its size, for formats that declare how big
// their data inflates to.
package inflate

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
)

// ErrTooLarge means data inflates past the size it was allowed.
var ErrTooLarge = errors.New("data inflates past its size")

// Zlib decompresses zlib data that inflates to at most limit bytes.
func Zlib(data []byte, limit int) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(io.LimitReader(zr, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(out) > limit {
		return nil, ErrTooLarge
	}
	return out, nil
}
