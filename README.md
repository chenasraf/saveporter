# saveporter

**`saveporter`** converts emulator save states between emulators. Save a game in RomM's web player,
pick it up in RetroArch on your Steam Deck, carry it over to the mGBA app, and back.

![Release](https://img.shields.io/github/v/release/chenasraf/saveporter)
![Downloads](https://img.shields.io/github/downloads/chenasraf/saveporter/total)
![License](https://img.shields.io/github/license/chenasraf/saveporter)

---

## 🚀 Features

- **Detects the input**: compressed or plain RetroArch states, EmulatorJS states, the mGBA app's PNG
  states and raw core states
- **Picks the target from the output's name**, or from `--to`
- **Rewrites newer states for older emulators**: a state from a newer core is rewritten in the
  layout the older one reads, so it loads instead of being refused
- **Batch conversion** of files and whole folders, mapping slots between emulators
- **Never overwrites** an existing file unless you say so, with a dry run to check first
- **A Go library** for programs that sync or manage saves

### Supported

| System           | Emulators                                                        |
| ---------------- | ---------------------------------------------------------------- |
| Game Boy Advance | RetroArch (mGBA core), EmulatorJS (mGBA core), the mGBA app 0.10 |

---

## 🎯 Installation

### Download Precompiled Binaries

Grab the latest release for **Linux**, **macOS**, or **Windows**:

- [Releases →](https://github.com/chenasraf/saveporter/releases/latest)

### Homebrew (macOS/Linux)

```bash
brew install chenasraf/tap/saveporter
```

### Go

```bash
go install github.com/chenasraf/saveporter/cmd/saveporter@latest
```

---

## ✨ Usage

### Convert a state

```bash
saveporter "Pokemon SoulGold.state" "Pokemon SoulGold.ss1"
```

```
Pokemon SoulGold.state → Pokemon SoulGold.ss1
  GBA · mGBA state v9 → v7 · RetroArch (rzip-deflate) → mGBA app
```

The target comes from the output's name:

| Output name                                  | Target                                              |
| -------------------------------------------- | --------------------------------------------------- |
| `.ss0`–`.ss9`                                | the mGBA app                                        |
| `.state`, `.state1`–`.state9`, `.state.auto` | RetroArch and EmulatorJS: a state both of them load |

Any other name needs `--to`. `-` reads stdin or writes stdout (writing needs `--to`).

### Convert many

```bash
saveporter --to mgba -o ~/mgba/states ~/Emulation/saves/retroarch/states
```

With `-o`, every input is converted into the directory. Directories are searched for state files;
anything that isn't a state saveporter knows is skipped and reported. Slots carry over:

| RetroArch / EmulatorJS      | mGBA app              |
| --------------------------- | --------------------- |
| `Game.state.auto`           | `Game.ss0`            |
| `Game.state`                | `Game.ss0`            |
| `Game.state1`–`Game.state9` | `Game.ss1`–`Game.ss9` |

The mGBA app's slot 0 is its auto-state, loaded when a game starts. When both `Game.state.auto` and
`Game.state` would become `Game.ss0`, the auto-state wins.

### See what a file is

```bash
saveporter info "Pokemon SoulGold.state"
```

```
Pokemon SoulGold.state
  system     GBA (mGBA core)
  state      version 9
  container  RetroArch, rzip-deflate, written by RetroArch
  game       POKEMON EMER (BPEE), ROM CRC 27b2e7ae
```

### Options

| Flag                    | Description                                                  |
| ----------------------- | ------------------------------------------------------------ |
| `-t`, `--to <emulator>` | Target: `retroarch`, `emulatorjs` or `mgba`                  |
| `--state-version <n>`   | Write at most this core state version, or `keep` the input's |
| `-o`, `--out-dir <dir>` | Convert every input into this directory                      |
| `-f`, `--force`         | Overwrite existing outputs                                   |
| `-n`, `--dry-run`       | Show what would be written, write nothing                    |
| `-q`, `--quiet`         | Report errors only                                           |
| `--json`                | Report as JSON (also on `info`)                              |

`saveporter` exits 0 when every input was converted or skipped by rule (an existing output, a file
that isn't a state), 1 when any input failed, and 2 on a usage mistake.

---

## 🧩 How it works

RetroArch, EmulatorJS and the mGBA app all run the same mGBA core, so a state is the same core data
in different wrappers: RetroArch's `RASTATE` container (which EmulatorJS writes too, and RetroArch
may compress), the mGBA app's PNG screenshot with the state inside, or the core's state alone.

The core's state layout carries a version. mGBA loads states of its own version or older and refuses
newer ones, so a state from a newer mGBA is rewritten in the older layout. Each newer version only
gave meaning to bytes older versions leave zero or ignore, so the older emulator loads it, and the
newer one reads the extra details back after a round trip.

| Target     | State version written                                           |
| ---------- | --------------------------------------------------------------- |
| EmulatorJS | at most 7, what EmulatorJS 4.2.3 (the release RomM ships) loads |
| mGBA app   | at most 7, what the 0.10 releases load                          |
| RetroArch  | the input's own: RetroArch's cores load older states            |

`--state-version` sets another version for an emulator older or newer than these.

---

## 📦 Go library

```bash
go get github.com/chenasraf/saveporter
```

```go
info, err := saveporter.Detect(data)
// info.System, info.StateVersion, info.Writers, info.ROMCRC, info.Title ...

out, err := saveporter.Convert(data, saveporter.Target{Emulator: saveporter.EmulatorJS})
```

`ParseStateName`, `StateFileName` and `TargetForName` map file names and slots between emulators.
The formats themselves are in the `retroarch` and `mgba` packages.

---

## 🧪 Accuracy

Besides unit and fuzz tests, `test/realcore` runs conversions through the real emulators in Docker:
mGBA's libretro core at state versions 7 and 11, the mGBA 0.10.5 app, and optionally any other
libretro core, such as a Steam Deck's:

```bash
test/realcore/run.sh <states-dir> <roms-dir> [mgba_libretro.so]
```

For every state, with the game's clock frozen and scripted input, it checks that each converted
state loads in its target, that a downgrade matches byte for byte what the older mGBA itself writes,
that a round trip plays out identically to the original for thousands of frames, and that the mGBA
app's PNG states decode to exactly what it writes raw.

---

## 🛠️ Contributing

I am developing this package on my free time, so any support, whether code, issues, or just stars is
very helpful to sustaining its life. If you are feeling incredibly generous and would like to donate
just a small amount to help sustain this project, I would be very very thankful!

<a href='https://ko-fi.com/casraf' target='_blank'>
  <img height='36' style='border:0px;height:36px;'
    src='https://cdn.ko-fi.com/cdn/kofi1.png?v=3'
    alt='Buy Me a Coffee at ko-fi.com' />
</a>

I welcome any issues or pull requests on GitHub. If you find a bug, or would like a new feature,
don't hesitate to open an appropriate issue and I will do my best to reply promptly.

---

## 📜 License

MIT
