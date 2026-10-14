// Package retroarch reads and writes RetroArch save states: a libretro core's own state in a
// "RASTATE" container, which RetroArch may compress into an RZIP file. EmulatorJS, being built on
// RetroArch, writes the same container.
//
// RetroArch reads every shape, compressed or not, so states are written as a plain container.
package retroarch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/chenasraf/saveporter/internal/inflate"
	"github.com/klauspost/compress/zstd"
)

const (
	stateMagic   = "RASTATE"
	stateVersion = 1
	blockHeader  = 8

	rzipMagic        = "#RZIPv"
	rzipHeaderSize   = 20
	rzipMaxChunkSize = 64 << 20
	// rzipMaxTotal bounds what an RZIP file may declare it holds. The largest console states
	// (GameCube, PS2) are well under it.
	rzipMaxTotal = 256 << 20
	// rzipPrealloc bounds the buffer reserved up front from a header's say-so; past it the
	// output grows as chunks actually inflate.
	rzipPrealloc = 16 << 20
)

var (
	blockMem = []byte("MEM ")
	blockEnd = []byte("END ")
)

// ErrNotState means data isn't a save state RetroArch wrote.
var ErrNotState = errors.New("not a RetroArch save state")

// Compression is how a state file is compressed.
type Compression int

const (
	// Uncompressed is a container or core state as is.
	Uncompressed Compression = iota
	// RZIPDeflate is RZIP version 1: zlib chunks, as RetroArch 1.x releases write.
	RZIPDeflate
	// RZIPZstd is RZIP version 2: Zstandard frames.
	RZIPZstd
)

func (c Compression) String() string {
	switch c {
	case RZIPDeflate:
		return "rzip-deflate"
	case RZIPZstd:
		return "rzip-zstd"
	}
	return "none"
}

// File is a RetroArch state file taken apart.
type File struct {
	// Core is the libretro core's own state.
	Core []byte
	// Compression is how the file was compressed.
	Compression Compression
	// Container says the core state sat in a RASTATE container. States RetroArch wrote before
	// it had one are the core state alone.
	Container bool
}

// Read takes a RetroArch state file apart. Data that is neither compressed nor a container is
// returned as a bare core state, as RetroArch itself would load it.
func Read(data []byte) (File, error) {
	data, comp, err := unRZIP(data)
	if err != nil {
		return File{}, err
	}
	if !bytes.HasPrefix(data, []byte(stateMagic)) {
		return File{Core: data, Compression: comp}, nil
	}
	if len(data) < len(stateMagic)+1 || data[len(stateMagic)] != stateVersion {
		return File{}, fmt.Errorf("%w: unknown container version", ErrNotState)
	}
	for rest := data[len(stateMagic)+1:]; len(rest) >= blockHeader; {
		tag, size := rest[:4], int(binary.LittleEndian.Uint32(rest[4:8]))
		rest = rest[blockHeader:]
		if bytes.Equal(tag, blockEnd) {
			break
		}
		if size > len(rest) {
			return File{}, fmt.Errorf("%w: block %q runs past the end", ErrNotState, tag)
		}
		if bytes.Equal(tag, blockMem) {
			return File{Core: rest[:size], Compression: comp, Container: true}, nil
		}
		rest = rest[min(align(size), len(rest)):]
	}
	return File{}, fmt.Errorf("%w: no core state in it", ErrNotState)
}

// CoreState returns the core's state from a RetroArch state file.
func CoreState(data []byte) ([]byte, error) {
	f, err := Read(data)
	return f.Core, err
}

// Wrap puts a core's state in RetroArch's container, as RetroArch and EmulatorJS write it with
// compression off.
func Wrap(core []byte) []byte {
	out := make([]byte, 0, len(stateMagic)+1+2*blockHeader+align(len(core)))
	out = append(out, stateMagic...)
	out = append(out, stateVersion)
	out = append(out, blockMem...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(core))) //nolint:gosec // a core state is far below 4 GiB
	out = append(out, core...)
	out = append(out, make([]byte, align(len(core))-len(core))...)
	out = append(out, blockEnd...)
	return binary.LittleEndian.AppendUint32(out, 0)
}

// align rounds a block's size up to the 8 bytes RetroArch pads every block to.
func align(n int) int { return (n + 7) &^ 7 }

// unRZIP decompresses an RZIP file, and returns anything else as is.
func unRZIP(data []byte) ([]byte, Compression, error) {
	if len(data) < rzipHeaderSize || !bytes.HasPrefix(data, []byte(rzipMagic)) || data[7] != '#' {
		return data, Uncompressed, nil
	}
	comp := Compression(data[6])
	if comp != RZIPDeflate && comp != RZIPZstd {
		return data, Uncompressed, nil
	}
	chunkSize := binary.LittleEndian.Uint32(data[8:12])
	total := binary.LittleEndian.Uint64(data[12:20])
	if chunkSize == 0 || chunkSize > rzipMaxChunkSize || total == 0 || total > rzipMaxTotal {
		return nil, comp, fmt.Errorf("%w: bad RZIP header", ErrNotState)
	}

	var zd *zstd.Decoder
	if comp == RZIPZstd {
		var err error
		// A chunk inflates to at most the chunk size, so a frame claiming more is refused
		// before it's decoded.
		zd, err = zstd.NewReader(nil, zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderMaxMemory(uint64(chunkSize)), zstd.WithDecoderMaxWindow(uint64(chunkSize)))
		if err != nil {
			return nil, comp, err
		}
		defer zd.Close()
	}
	out := make([]byte, 0, min(total, rzipPrealloc))
	for rest := data[rzipHeaderSize:]; uint64(len(out)) < total; {
		if len(rest) < 4 {
			return nil, comp, fmt.Errorf("%w: RZIP file ends early", ErrNotState)
		}
		size := binary.LittleEndian.Uint32(rest[:4])
		rest = rest[4:]
		if size == 0 || uint64(size) > uint64(len(rest)) {
			return nil, comp, fmt.Errorf("%w: bad RZIP chunk", ErrNotState)
		}
		chunk := rest[:size]
		rest = rest[size:]
		var err error
		if zd != nil {
			// Decoded on its own: the decoder's limit counts whatever the destination holds.
			var decoded []byte
			decoded, err = zd.DecodeAll(chunk, nil)
			if err == nil && len(decoded) > int(chunkSize) {
				err = inflate.ErrTooLarge
			}
			out = append(out, decoded...)
		} else {
			var inflated []byte
			inflated, err = inflate.Zlib(chunk, int(chunkSize))
			out = append(out, inflated...)
		}
		if err != nil {
			return nil, comp, fmt.Errorf("%w: %w", ErrNotState, err)
		}
	}
	if uint64(len(out)) != total {
		return nil, comp, fmt.Errorf("%w: RZIP file holds %d bytes, not %d", ErrNotState, len(out), total)
	}
	return out, comp, nil
}
