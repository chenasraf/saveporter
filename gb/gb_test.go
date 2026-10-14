package gb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/chenasraf/saveporter/internal/fixtures"
)

var now = time.Unix(1760400000, 0)

func cart(t *testing.T, typ, rom, ram byte) Cart {
	t.Helper()
	c, err := ParseCart(fixtures.GBHeader(typ, rom, ram, true))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i>>8)
	}
	return b
}

func TestParseCart(t *testing.T) {
	c := cart(t, 0x10, 0x06, 0x03)
	if c.Mapper() != MBC3 || !c.Battery() || !c.Clock() || !c.CGB || c.Title != "FIXTURE" {
		t.Fatalf("%+v", c)
	}
	h := fixtures.GBHeader(0x10, 0x06, 0x03, false)
	h[0x14D]++
	if _, err := ParseCart(h); !errors.Is(err, ErrNotHeader) {
		t.Fatalf("bad checksum: %v", err)
	}
	if _, err := ParseCart(make([]byte, HeaderSize)); !errors.Is(err, ErrNotHeader) {
		t.Fatalf("no logo: %v", err)
	}
}

// Each format's RAM file for each kind of cartridge: size, padding, layout.
func TestRAMFiles(t *testing.T) {
	tests := []struct {
		name           string
		typ, rom, ram  byte
		format         Format
		size           int
		padFrom, padTo int
	}{
		{"8 KiB", 0x03, 0x04, 0x02, Gambatte, 0x2000, 0, 0},
		{"2 KiB Gambatte", 0x03, 0x04, 0x01, Gambatte, 0x2000, 0x800, 0x2000},
		{"2 KiB SameBoy", 0x03, 0x04, 0x01, SameBoy, 0x800, 0, 0},
		{"2 KiB VBA-M", 0x03, 0x04, 0x01, VBAM, 0x800, 0, 0},
		{"2 KiB Gearboy", 0x03, 0x04, 0x01, Gearboy, 0x2000, 0x800, 0x2000},
		{"2 KiB mGBA", 0x03, 0x04, 0x01, MGBA, 0x2000, 0x800, 0x2000},
		{"8 KiB MBC3 Gearboy", 0x13, 0x05, 0x02, Gearboy, 0x8000, 0x2000, 0x8000},
		{"64 KiB Gambatte", 0x1B, 0x06, 0x05, Gambatte, 0x20000, 0x10000, 0x20000},
		{"64 KiB mGBA", 0x1B, 0x06, 0x05, MGBA, 0x10000, 0, 0},
		{"128 KiB", 0x1B, 0x06, 0x04, MGBAApp, 0x20000, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := cart(t, tt.typ, tt.rom, tt.ram)
			n, _ := c.SaveSize()
			ram := pattern(n)
			files, err := Write(tt.format, c, Save{RAM: ram}, now)
			if err != nil {
				t.Fatal(err)
			}
			file := files[ExtSRM]
			if tt.format == MGBAApp {
				file = files[ExtSav]
			}
			if len(file) != tt.size || !bytes.Equal(file[:n], ram) {
				t.Fatalf("file %d bytes, RAM kept: %v", len(file), bytes.Equal(file[:min(n, len(file))], ram))
			}
			if !bytes.Equal(file[tt.padFrom:tt.padTo], bytes.Repeat([]byte{0xFF}, tt.padTo-tt.padFrom)) {
				t.Fatal("padding isn't erased RAM")
			}
			if _, ok := files[ExtRTC]; ok {
				t.Fatal("a clock file for a cartridge without a clock")
			}
			back, err := Read(tt.format, c, files, now)
			if err != nil || !bytes.Equal(back.RAM, ram) {
				t.Fatalf("read back: %v", err)
			}
		})
	}
}

func TestMBC2(t *testing.T) {
	c := cart(t, 0x06, 0x03, 0x00)
	ram := make([]byte, mbc2Cells)
	for i := range ram {
		ram[i] = 0xF0 | byte(i*5)&0xF
	}
	for _, f := range []Format{Gambatte, SameBoy, VBAM} {
		files, err := Write(f, c, Save{RAM: ram}, now)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(files[ExtSRM][:mbc2Cells], ram) {
			t.Fatalf("%s: half-bytes not one to a byte", f)
		}
	}
	files, err := Write(MGBA, c, Save{RAM: ram}, now)
	if err != nil {
		t.Fatal(err)
	}
	srm := files[ExtSRM]
	if len(srm) != 0x100 || srm[0] != (ram[0]&0xF|ram[1]<<4) || srm[0xFF] != (ram[0x1FE]&0xF|ram[0x1FF]<<4) {
		t.Fatalf("mGBA's packing: % x", srm[:4])
	}
	back, err := Read(MGBA, c, files, now)
	if err != nil || !bytes.Equal(back.RAM, ram) {
		t.Fatalf("mGBA read back: %v", err)
	}
	if _, err := Write(Gearboy, c, Save{RAM: ram}, now); !errors.Is(err, ErrUnsupportedCart) {
		t.Fatalf("Gearboy keeps no MBC2 save: %v", err)
	}
}

func TestMBC7WordOrder(t *testing.T) {
	c := cart(t, 0x22, 0x05, 0x00)
	ram := pattern(0x100)
	sb, err := Write(SameBoy, c, Save{RAM: ram}, now)
	if err != nil {
		t.Fatal(err)
	}
	if sb[ExtSRM][0] != ram[1] || sb[ExtSRM][1] != ram[0] {
		t.Fatal("SameBoy's words aren't little-endian")
	}
	mg, err := Write(MGBA, c, Save{RAM: ram}, now)
	if err != nil || !bytes.Equal(mg[ExtSRM], ram) {
		t.Fatalf("mGBA's words aren't big-endian: %v", err)
	}
	back, err := Read(SameBoy, c, sb, now)
	if err != nil || !bytes.Equal(back.RAM, ram) {
		t.Fatalf("SameBoy read back: %v", err)
	}
	if _, err := Write(Gambatte, c, Save{RAM: ram}, now); !errors.Is(err, ErrUnsupportedCart) {
		t.Fatalf("Gambatte runs no MBC7: %v", err)
	}
}

func TestShortFileReadsAsErased(t *testing.T) {
	c := cart(t, 0x1B, 0x05, 0x03)
	s, err := Read(SameBoy, c, Files{ExtSRM: []byte{1, 2, 3}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.RAM) != 0x8000 || s.RAM[2] != 3 || s.RAM[3] != 0xFF {
		t.Fatalf("% x", s.RAM[:4])
	}
}

func TestNoBattery(t *testing.T) {
	if _, err := Read(Gambatte, cart(t, 0x19, 0x05, 0x03), Files{}, now); !errors.Is(err, ErrNoSave) {
		t.Fatal(err)
	}
}

// The clock reads the same in every format at the time it's carried over, and runs on from there.
func TestClockAcrossFormats(t *testing.T) {
	c := cart(t, 0x10, 0x06, 0x03)
	seconds := int64(300*day + 23*3600 + 59*60 + 50)
	gambatte := Files{
		ExtSRM: pattern(0x8000),
		ExtRTC: binary.LittleEndian.AppendUint64(nil, uint64(now.Unix()-seconds)),
	}
	src, err := Read(Gambatte, c, gambatte, now)
	if err != nil {
		t.Fatal(err)
	}
	want := clockAt(seconds, now)
	if *src.Clock != want {
		t.Fatalf("Gambatte's clock reads %+v, want %+v", *src.Clock, want)
	}
	later := now.Add(93784 * time.Second)
	for _, f := range Formats {
		files, err := Write(f, c, src, now)
		if err != nil {
			t.Fatal(err)
		}
		s, err := Read(f, c, files, now)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.Clock.At(later); got != want.At(later) {
			t.Errorf("%s: clock reads %+v later, want %+v", f, got, want.At(later))
		}
		if f == Gambatte && !bytes.Equal(files[ExtRTC], gambatte[ExtRTC]) {
			t.Error("Gambatte's clock changed through the conversion")
		}
	}
}

func TestClockDays(t *testing.T) {
	c := Clock{Days: 511, Hours: 23, Minutes: 59, Seconds: 59, Time: now}
	next := c.At(now.Add(time.Second))
	if next.Days != 0 || !next.Carry || next.Seconds != 0 {
		t.Fatalf("day 512: %+v", next)
	}
	halted := Clock{Days: 3, Halt: true, Time: now}
	if h := halted.At(now.Add(time.Hour)); h.Days != 3 || h.Hours != 0 {
		t.Fatalf("a halted clock ran: %+v", h)
	}
	// The footer's days register holds the 9-bit count; DH's bit 0 is its high bit.
	f := writeFooter(Clock{Days: 300, Carry: true, Time: now})
	if d := binary.LittleEndian.Uint32(f[12:]); d != 300 {
		t.Fatalf("days register %d", d)
	}
	if dh := binary.LittleEndian.Uint32(f[16:]); dh != dhDayBit8|dhCarry {
		t.Fatalf("DH %#x", dh)
	}
	// A days register with only the low byte reads the same.
	binary.LittleEndian.PutUint32(f[12:], 300&0xFF)
	if got := readFooter(f, false); got.Days != 300 {
		t.Fatalf("low-byte days read as %d", got.Days)
	}
}

// Any files, read as any format for any cartridge, either fail or write every format back.
func FuzzReadWrite(f *testing.F) {
	f.Add(byte(0x10), byte(0x03), []byte{1, 2, 3}, []byte{4, 5, 6, 7, 8, 9, 10, 11})
	f.Add(byte(0xFE), byte(0x03), []byte{}, make([]byte, huc3Footer))
	f.Add(byte(0x06), byte(0x00), make([]byte, 0x100), []byte{})
	f.Fuzz(func(t *testing.T, typ, ram byte, srm, rtc []byte) {
		c, err := ParseCart(fixtures.GBHeader(typ, 0x05, ram, true))
		if err != nil {
			return
		}
		for _, from := range Formats {
			files := Files{ExtSRM: srm, ExtRTC: rtc, ExtSav: append(slices.Clone(srm), rtc...)}
			s, err := Read(from, c, files, now)
			if err != nil {
				continue
			}
			for _, to := range Formats {
				if _, err := Write(to, c, s, now); err != nil && !errors.Is(err, ErrUnsupportedCart) {
					t.Fatalf("%s → %s: %v", from, to, err)
				}
			}
		}
	})
}

func TestHuC3(t *testing.T) {
	c := cart(t, 0xFE, 0x05, 0x03)
	base := Files{ExtSRM: pattern(0x8000), ExtRTC: binary.LittleEndian.AppendUint64(nil, uint64(now.Unix()-(3*day+125*60+17)))}
	s, err := Read(Gambatte, c, base, now)
	if err != nil {
		t.Fatal(err)
	}
	if s.HuC3.Days != 3 || s.HuC3.Minutes != 125 {
		t.Fatalf("%+v", *s.HuC3)
	}
	app, err := Write(MGBAApp, c, s, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(app[ExtSav]) != 0x8000+huc3Footer {
		t.Fatalf("mGBA app save %d bytes", len(app[ExtSav]))
	}
	back, err := Read(MGBAApp, c, app, now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Write(Gambatte, c, back, now)
	if err != nil || !bytes.Equal(g[ExtRTC], base[ExtRTC]) {
		t.Fatalf("HuC3 clock through the mGBA app: %v", err)
	}
	if mg, _ := Write(MGBA, c, s, now); mg[ExtRTC] != nil {
		t.Fatal("mGBA's libretro core keeps no HuC3 clock")
	}
}
