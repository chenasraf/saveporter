package gb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"testing"
	"time"
)

// mgbaFooter is the clock as mGBA writes it at flush: live registers advanced to then, latched ones
// as of the last latch, and the latch's time.
func mgbaFooter(latched Clock, flush time.Time) []byte {
	live := writeFooter(latched.At(flush))
	return append(append(live[:20:20], writeFooter(latched)[20:40]...), writeFooter(latched)[40:]...)
}

func random(n int) []byte {
	r := rand.New(rand.NewPCG(1, uint64(n)))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// Without the header, a conversion goes through exactly when every cartridge the files could be
// for comes out the same, and then matches the conversion with the header.
func TestConvertWithoutHeader(t *testing.T) {
	baseTime := binary.LittleEndian.AppendUint64(nil, uint64(now.Unix()-1000))
	tests := []struct {
		name     string
		cart     Cart
		from, to Format
		files    Files
		ok       bool
	}{
		{"8 KiB RAM", Cart{Type: 0x1B, RAMSize: 2}, Gambatte, SameBoy, Files{ExtSRM: random(0x2000)}, true},
		{"8 KiB RAM to the mGBA app", Cart{Type: 0x1B, RAMSize: 2}, Gambatte, MGBAApp, Files{ExtSRM: random(0x2000)}, true},
		{"32 KiB to Gearboy", Cart{Type: 0x1B, RAMSize: 3}, Gambatte, Gearboy, Files{ExtSRM: random(0x8000)}, true},
		{"2 KiB, exact", Cart{Type: 0x03, RAMSize: 1}, SameBoy, Gambatte, Files{ExtSRM: random(0x800)}, true},
		// Erased past 0x200 could be MBC2's, which mGBA packs.
		{"MBC2 or RAM, to mGBA", Cart{Type: 0x06}, Gambatte, MGBA,
			Files{ExtSRM: append(random(0x200), bytes.Repeat([]byte{0xFF}, 0x1E00)...)}, false},
		// MBC2 and RAM are stored alike in SameBoy.
		{"MBC2 or RAM, to SameBoy", Cart{Type: 0x06}, Gambatte, SameBoy,
			Files{ExtSRM: append(random(0x200), bytes.Repeat([]byte{0xFF}, 0x1E00)...)}, true},
		{"MBC2 by its size", Cart{Type: 0x06}, VBAM, MGBA, Files{ExtSRM: random(0x200)}, false},
		{"MBC7 by its size", Cart{Type: 0x22, ROMSize: 0x05}, SameBoy, MGBA, Files{ExtSRM: random(0x100)}, true},
		{"mGBA's 256 bytes are MBC2's or MBC7's", Cart{Type: 0x22, ROMSize: 0x05}, MGBA, SameBoy, Files{ExtSRM: random(0x100)}, false},
		// Gambatte's clock is MBC3's or HuC3's.
		{"Gambatte's clock", Cart{Type: 0x10, RAMSize: 3}, Gambatte, MGBAApp, Files{ExtSRM: random(0x8000), ExtRTC: baseTime}, false},
		{"Gambatte's clock to Gambatte", Cart{Type: 0x10, RAMSize: 3}, Gambatte, Gambatte, Files{ExtSRM: random(0x8000), ExtRTC: baseTime}, true},
		// VBA-M's 48 bytes are MBC3's only.
		{"VBA-M's clock", Cart{Type: 0x10, RAMSize: 3}, VBAM, MGBAApp, Files{ExtSRM: random(0x8000), ExtRTC: writeFooter(Clock{Days: 3, Time: now})}, true},
		// The app writes its live registers as of the write and the time of the last latch: not
		// what saveporter writes, but the same cartridge.
		{"the mGBA app's own clock", Cart{Type: 0x10, RAMSize: 3}, MGBAApp, VBAM,
			Files{ExtSav: append(random(0x8000), mgbaFooter(Clock{Days: 3, Time: now.Add(-time.Hour)}, now)...)}, true},
		// Gearboy pads every MBC3's RAM to 32 KiB: where the mGBA app's clock goes after it isn't known.
		{"Gearboy's MBC3 clock to the app", Cart{Type: 0x10, RAMSize: 2}, Gearboy, MGBAApp,
			Files{ExtSRM: append(random(0x2000), bytes.Repeat([]byte{0xFF}, 0x6000)...), ExtRTC: writeFooter(Clock{Days: 3, Time: now})}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Convert(tt.files, tt.from, tt.to, nil, now)
			if !tt.ok {
				if !errors.Is(err, ErrNeedHeader) {
					t.Fatalf("got %v, want ErrNeedHeader", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want, err := Convert(tt.files, tt.from, tt.to, &tt.cart, now)
			if err != nil {
				t.Fatal(err)
			}
			if !splitOutput(tt.to, tt.cart, got).loadsLike(splitOutput(tt.to, tt.cart, want)) {
				t.Fatal("loads differently from the conversion with the header")
			}
		})
	}
}

func TestConvertWithoutHeaderRefuses(t *testing.T) {
	if _, err := Convert(Files{ExtSRM: random(0x100)}, SameBoy, Gambatte, nil, now); !errors.Is(err, ErrUnsupportedCart) {
		t.Fatalf("SameBoy's MBC7 save to Gambatte: %v", err)
	}
	if _, err := Convert(Files{ExtSRM: random(0x1234)}, Gambatte, MGBA, nil, now); !errors.Is(err, ErrNotSave) {
		t.Fatalf("a size Gambatte never writes: %v", err)
	}
}
