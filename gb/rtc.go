package gb

import (
	"encoding/binary"
	"time"
)

// Clock is an MBC3 cartridge's real-time clock: its registers as they read at Time.
//
// A cartridge clock runs on while the game is off, so every emulator stores the registers with the
// moment they held, or the moment the clock read zero, and catches up from the wall clock when the
// game loads.
type Clock struct {
	Seconds, Minutes, Hours byte
	// Days is the 9-bit day counter.
	Days uint16
	// Halt stops the clock; Carry is set when the day counter overflows, until the game clears it.
	Halt, Carry bool
	Time        time.Time
}

const (
	day          = 24 * 60 * 60
	clockDays    = 512
	dhDayBit8    = 0x01
	dhHalt       = 0x40
	dhCarry      = 0x80
	footerSize   = 48
	sameBoyRTC   = 32
	baseTimeSize = 8
)

func (c Clock) seconds() int64 {
	return int64(c.Days)*day + int64(c.Hours)*3600 + int64(c.Minutes)*60 + int64(c.Seconds)
}

// clockAt is the clock that has counted n seconds since it read zero.
func clockAt(n int64, t time.Time) Clock {
	c := Clock{Time: t}
	if n < 0 {
		n = 0
	}
	days := n / day
	if days >= clockDays {
		c.Carry = true
		days %= clockDays
	}
	c.Days = uint16(days)
	c.Hours, c.Minutes, c.Seconds = byte(n%day/3600), byte(n%3600/60), byte(n%60)
	return c
}

// At is the clock as it reads at t. A halted clock reads the same at any time.
func (c Clock) At(t time.Time) Clock {
	if c.Halt {
		c.Time = t
		return c
	}
	n := clockAt(c.seconds()+int64(t.Sub(c.Time)/time.Second), t)
	n.Carry = n.Carry || c.Carry
	return n
}

func (c Clock) dh() byte {
	dh := byte(c.Days>>8) & dhDayBit8
	if c.Halt {
		dh |= dhHalt
	}
	if c.Carry {
		dh |= dhCarry
	}
	return dh
}

func clockFromRegisters(s, m, h byte, days uint32, dh byte, t time.Time) Clock {
	return Clock{
		Seconds: s, Minutes: m, Hours: h,
		// Gearboy keeps the whole 9-bit count in the days register, the others the low 8 bits; the
		// high bit is in DH either way.
		Days: uint16(days&0xFF) | uint16(dh&dhDayBit8)<<8,
		Halt: dh&dhHalt != 0, Carry: dh&dhCarry != 0, Time: t,
	}
}

// readFooter reads the 48-byte clock VBA-M, Gearboy and mGBA write: the live registers, the latched
// registers (each five u32s: seconds, minutes, hours, days, DH), then the unix time they hold at.
// VBA-M and Gearboy keep the live registers as of that time. mGBA keeps the latched ones as of it
// and the live ones as of when it wrote the file, and reads back only the latched ones.
func readFooter(b []byte, latched bool) Clock {
	r := b[:20]
	if latched {
		r = b[20:40]
	}
	u := func(i int) uint32 { return binary.LittleEndian.Uint32(r[4*i:]) }
	t := time.Unix(int64(binary.LittleEndian.Uint64(b[40:])), 0)
	return clockFromRegisters(byte(u(0)), byte(u(1)), byte(u(2)), u(3), byte(u(4)), t)
}

// writeFooter writes the 48-byte clock with both register sets as of the clock's time, which every
// reader of it takes for what it is. The days registers hold the whole 9-bit count, which Gearboy
// counts on and the others mask.
func writeFooter(c Clock) []byte {
	regs := []uint32{uint32(c.Seconds), uint32(c.Minutes), uint32(c.Hours), uint32(c.Days), uint32(c.dh())}
	b := make([]byte, 0, footerSize)
	for range 2 {
		for _, r := range regs {
			b = binary.LittleEndian.AppendUint32(b, r)
		}
	}
	return binary.LittleEndian.AppendUint64(b, uint64(c.Time.Unix()))
}

// readSameBoy reads SameBoy's 32-byte clock: the live registers (seconds, minutes, hours, days low
// byte, DH), the latched ones, padding, then the unix second the live ones hold at, a sub-second
// cycle count and a TPP1 register.
func readSameBoy(b []byte) Clock {
	t := time.Unix(int64(binary.LittleEndian.Uint64(b[16:])), 0)
	return clockFromRegisters(b[0], b[1], b[2], uint32(b[3]), b[4], t)
}

func writeSameBoy(c Clock) []byte {
	b := make([]byte, sameBoyRTC)
	b[0], b[1], b[2], b[3], b[4] = c.Seconds, c.Minutes, c.Hours, byte(c.Days), c.dh()
	copy(b[5:10], b[0:5])
	binary.LittleEndian.PutUint64(b[16:], uint64(c.Time.Unix()))
	return b
}

// readBaseTime reads Gambatte's clock, the unix time at which it read zero: the clock as it reads
// at now. Gambatte keeps neither halt nor carry.
func readBaseTime(b []byte, now time.Time) Clock {
	base := int64(binary.LittleEndian.Uint64(b))
	return clockAt(now.Unix()-base, now)
}

// writeBaseTime writes Gambatte's clock. A halted clock runs on from now, and the carry is lost.
func writeBaseTime(c Clock, now time.Time) []byte {
	c = c.At(now)
	return binary.LittleEndian.AppendUint64(nil, uint64(now.Unix()-c.seconds()))
}

// HuC3Clock is a HuC3 cartridge's clock: minutes into the day and a 12-bit day count, as they read
// at Time.
type HuC3Clock struct {
	Minutes, Days uint16
	Time          time.Time
	// Registers are the mGBA app's whole register file, alarm and tone included, kept for writing
	// back to it. Nil from any other emulator.
	Registers *[huc3Registers]byte
}

const (
	minutesPerDay = 1440
	huc3Days      = 0x1000
	huc3Registers = 0x100
	huc3Footer    = huc3Registers/2 + 8
	huc3Minutes   = 0x10
	huc3Days0     = 0x13
)

// At is the clock as it reads at t, which counts whole minutes from its time.
func (h HuC3Clock) At(t time.Time) HuC3Clock {
	elapsed := max(int64(t.Sub(h.Time)/time.Second)/60, 0)
	total := int64(h.Days)*minutesPerDay + int64(h.Minutes) + elapsed
	h.Minutes, h.Days = uint16(total%minutesPerDay), uint16(total/minutesPerDay%huc3Days)
	h.Time = h.Time.Add(time.Duration(elapsed) * time.Minute)
	return h
}

// readHuC3BaseTime reads Gambatte's and VBA-M's HuC3 clock, the unix time it read zero.
func readHuC3BaseTime(b []byte, now time.Time) HuC3Clock {
	n := max(now.Unix()-int64(binary.LittleEndian.Uint64(b)), 0)
	minutes := n / 60
	return HuC3Clock{
		Minutes: uint16(minutes % minutesPerDay),
		Days:    uint16(minutes / minutesPerDay % huc3Days),
		Time:    now.Add(-time.Duration(n%60) * time.Second),
	}
}

func writeHuC3BaseTime(h HuC3Clock) []byte {
	base := h.Time.Unix() - int64(h.Days)*day - int64(h.Minutes)*60
	return binary.LittleEndian.AppendUint64(nil, uint64(base))
}

// readHuC3Footer reads the mGBA app's HuC3 footer: its 256 four-bit registers two to a byte, low
// half first, then the unix time the clock registers hold at.
func readHuC3Footer(b []byte) HuC3Clock {
	var regs [huc3Registers]byte
	for i, v := range b[:huc3Registers/2] {
		regs[2*i], regs[2*i+1] = v&0xF, v>>4
	}
	nibbles := func(at int) uint16 {
		return uint16(regs[at]) | uint16(regs[at+1])<<4 | uint16(regs[at+2])<<8
	}
	return HuC3Clock{
		Minutes:   nibbles(huc3Minutes),
		Days:      nibbles(huc3Days0),
		Time:      time.Unix(int64(binary.LittleEndian.Uint64(b[huc3Registers/2:])), 0),
		Registers: &regs,
	}
}

func writeHuC3Footer(h HuC3Clock) []byte {
	var regs [huc3Registers]byte
	if h.Registers != nil {
		regs = *h.Registers
	}
	for i := range 3 {
		regs[huc3Minutes+i] = byte(h.Minutes>>(4*i)) & 0xF
		regs[huc3Days0+i] = byte(h.Days>>(4*i)) & 0xF
	}
	b := make([]byte, 0, huc3Footer)
	for i := 0; i < huc3Registers; i += 2 {
		b = append(b, regs[i]|regs[i+1]<<4)
	}
	return binary.LittleEndian.AppendUint64(b, uint64(h.Time.Unix()))
}
