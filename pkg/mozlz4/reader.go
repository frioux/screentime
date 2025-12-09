// Package mozlz4 provides a reader for Mozilla's mozLz4 compressed files.
// These files have a magic header "mozLz40\0" followed by LZ4 block-compressed data.
package mozlz4

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/pierrec/lz4/v4"
)

var mozLz4Magic = []byte("mozLz40\x00")

// Reader wraps an io.Reader to decompress mozLz4 data
type Reader struct {
	r      io.Reader
	buf    *bytes.Reader
	inited bool
}

// NewReader creates a new mozLz4 reader
func NewReader(r io.Reader) (*Reader, error) {
	return &Reader{r: r}, nil
}

func (r *Reader) init() error {
	if r.inited {
		return nil
	}
	r.inited = true

	// Read the entire input
	data, err := io.ReadAll(r.r)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}

	// Check magic header
	if len(data) < 12 {
		return fmt.Errorf("data too short")
	}
	if !bytes.Equal(data[:8], mozLz4Magic) {
		return fmt.Errorf("invalid mozLz4 magic header")
	}

	// Read uncompressed size (4 bytes, little-endian)
	uncompressedSize := binary.LittleEndian.Uint32(data[8:12])
	compressed := data[12:]

	// Decompress
	decompressed := make([]byte, uncompressedSize)
	n, err := lz4.UncompressBlock(compressed, decompressed)
	if err != nil {
		return fmt.Errorf("decompress: %w", err)
	}

	r.buf = bytes.NewReader(decompressed[:n])
	return nil
}

// Read implements io.Reader
func (r *Reader) Read(p []byte) (int, error) {
	if err := r.init(); err != nil {
		return 0, err
	}
	return r.buf.Read(p)
}


