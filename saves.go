package saveporter

import (
	"time"

	"github.com/chenasraf/saveporter/gb"
)

// ConvertSave converts a Game Boy or Game Boy Color battery save from one emulator's files to
// another's. header is the start of the game's ROM (at least gb.HeaderSize bytes), which says what
// the save holds. It may be nil: the files then say what they can, and when they don't say enough
// for this conversion it fails with gb.ErrNeedHeader. now is the time the cartridge clock is
// carried over at: the clock reads in the output what it reads in the input at now, and runs on
// from there.
//
// EmulatorJS runs the same cores as RetroArch and keeps the same ".srm", but no ".rtc": convert its
// saves from and to the core's format and leave the ".rtc" out.
func ConvertSave(files gb.Files, from, to gb.Format, header []byte, now time.Time) (gb.Files, error) {
	if header == nil {
		return gb.Convert(files, from, to, nil, now)
	}
	cart, err := gb.ParseCart(header)
	if err != nil {
		return nil, err
	}
	return gb.Convert(files, from, to, &cart, now)
}
