package g733

import (
	"fmt"
	"strings"

	"github.com/felipearomani/g733-control/internal/i18n"
)

// EffectID is the effect identifier defined by HID++ feature 0x8070.
type EffectID uint16

const (
	EffectOff     EffectID = 0x0000
	EffectStatic  EffectID = 0x0001
	EffectPulse   EffectID = 0x0002
	EffectCycle   EffectID = 0x0003
	EffectBreathe EffectID = 0x000A
)

func (e EffectID) String() string {
	switch e {
	case EffectOff:
		return "off"
	case EffectStatic:
		return "static"
	case EffectPulse:
		return "pulse"
	case EffectCycle:
		return "cycle"
	case EffectBreathe:
		return "breathe"
	}
	return fmt.Sprintf("0x%04x", uint16(e))
}

// Label is the translated UI name of the effect.
func (e EffectID) Label() string {
	switch e {
	case EffectOff:
		return i18n.T("Off")
	case EffectStatic:
		return i18n.T("Static")
	case EffectBreathe:
		return i18n.T("Breathe")
	case EffectCycle:
		return i18n.T("Color cycle")
	case EffectPulse:
		return i18n.T("Pulse")
	}
	return e.String()
}

// UsesColor reports whether the effect has a color parameter.
func (e EffectID) UsesColor() bool {
	return e == EffectStatic || e == EffectBreathe || e == EffectPulse
}

// UsesPeriod reports whether the effect has a speed/period parameter.
func (e EffectID) UsesPeriod() bool { return e == EffectBreathe || e == EffectCycle }

// DefaultPeriod is the effect period used when none is known.
func (e EffectID) DefaultPeriod() int {
	if e == EffectCycle {
		return 10000
	}
	return 4000
}

// Zone is one LED zone and the effects it supports.
type Zone struct {
	Index    byte
	Location uint16
	Effects  []EffectID
	slots    map[EffectID]byte // effect ID -> effect index in this zone
}

// ID is a stable, untranslated identifier (logo, strips, zone2...).
func (z Zone) ID() string {
	switch z.Location & 0xFF {
	case 0x01:
		return "strips"
	case 0x02:
		return "logo"
	}
	return fmt.Sprintf("zone%d", z.Index)
}

// Name is the translated zone name derived from its location code.
func (z Zone) Name() string {
	switch z.Location & 0xFF {
	case 0x01:
		return i18n.T("Strips")
	case 0x02:
		return i18n.T("Logo")
	}
	return i18n.Tf("Zone %d", z.Index)
}

func (z Zone) Supports(e EffectID) bool {
	_, ok := z.slots[e]
	return ok
}

// RGB is a 24-bit color.
type RGB struct{ R, G, B uint8 }

func (c RGB) Hex() string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }

// Scale multiplies the color by pct/100.
func (c RGB) Scale(pct int) RGB {
	pct = clamp(pct, 0, 100)
	f := func(v uint8) uint8 { return uint8((int(v)*pct + 50) / 100) }
	return RGB{f(c.R), f(c.G), f(c.B)}
}

// NamedColor is a palette entry; both names are accepted as input.
type NamedColor struct {
	EN, PT string
	Color  RGB
}

// Name is the color name in the active language.
func (n NamedColor) Name() string {
	if i18n.Current() == i18n.PT {
		return n.PT
	}
	return n.EN
}

// NamedColors is the palette, accepted wherever a color is parsed.
var NamedColors = []NamedColor{
	{"red", "vermelho", RGB{0xFF, 0x00, 0x00}},
	{"orange", "laranja", RGB{0xFF, 0x5A, 0x00}},
	{"yellow", "amarelo", RGB{0xFF, 0xD0, 0x00}},
	{"green", "verde", RGB{0x00, 0xFF, 0x30}},
	{"cyan", "ciano", RGB{0x00, 0xE5, 0xFF}},
	{"logitech", "logitech", RGB{0x00, 0xB6, 0xFF}},
	{"blue", "azul", RGB{0x00, 0x30, 0xFF}},
	{"purple", "roxo", RGB{0x8A, 0x2B, 0xE2}},
	{"magenta", "magenta", RGB{0xFF, 0x00, 0xC8}},
	{"pink", "rosa", RGB{0xFF, 0x4F, 0x9A}},
	{"white", "branco", RGB{0xFF, 0xFF, 0xFF}},
	{"warm", "quente", RGB{0xFF, 0xB4, 0x6B}},
}

// ParseColor accepts RRGGBB, #RRGGBB, RGB shorthand or a color name.
func ParseColor(s string) (RGB, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, nc := range NamedColors {
		if nc.EN == s || nc.PT == s {
			return nc.Color, nil
		}
	}
	h := strings.TrimPrefix(s, "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	var c RGB
	if len(h) != 6 {
		return c, i18n.Errorf("invalid color %q (use RRGGBB or a name)", s)
	}
	if _, err := fmt.Sscanf(h, "%02x%02x%02x", &c.R, &c.G, &c.B); err != nil {
		return c, i18n.Errorf("invalid color %q (use RRGGBB or a name)", s)
	}
	return c, nil
}

// Light is the full state of one zone.
type Light struct {
	Effect     EffectID
	Color      RGB // base color (before brightness)
	PeriodMS   int
	Brightness int // 0-100: scales the color for static, intensity otherwise
}

func (l Light) String() string {
	switch l.Effect {
	case EffectOff:
		return l.Effect.Label()
	case EffectStatic:
		return fmt.Sprintf("%s %s %d%%", l.Effect.Label(), l.Color.Hex(), l.Brightness)
	case EffectBreathe:
		return fmt.Sprintf("%s %s %.1fs %d%%", l.Effect.Label(), l.Color.Hex(), float64(l.PeriodMS)/1000, l.Brightness)
	case EffectCycle:
		return fmt.Sprintf("%s %.1fs %d%%", l.Effect.Label(), float64(l.PeriodMS)/1000, l.Brightness)
	}
	return l.Effect.Label()
}

// encodeLight builds the 10-byte effect parameter block of setZoneEffect.
func encodeLight(l Light) [10]byte {
	var p [10]byte
	bright := clamp(l.Brightness, 0, 100)
	period := uint16(clamp(l.PeriodMS, 0, 0xFFFF))
	if period == 0 {
		period = uint16(l.Effect.DefaultPeriod())
	}
	switch l.Effect {
	case EffectStatic:
		c := l.Color.Scale(bright)
		p[0], p[1], p[2] = c.R, c.G, c.B
	case EffectBreathe, EffectPulse:
		p[0], p[1], p[2] = l.Color.R, l.Color.G, l.Color.B
		p[3], p[4] = byte(period>>8), byte(period)
		p[5] = 0 // waveform: default
		p[6] = byte(bright)
	case EffectCycle:
		p[5], p[6] = byte(period>>8), byte(period)
		p[7] = byte(bright)
	}
	return p
}

// decodeLight parses the parameter block returned by getZoneEffect.
func decodeLight(e EffectID, p []byte) Light {
	l := Light{Effect: e, Brightness: 100, PeriodMS: e.DefaultPeriod()}
	if len(p) < 10 {
		return l
	}
	switch e {
	case EffectStatic:
		l.Color = RGB{p[0], p[1], p[2]}
	case EffectBreathe, EffectPulse:
		l.Color = RGB{p[0], p[1], p[2]}
		if v := int(p[3])<<8 | int(p[4]); v > 0 {
			l.PeriodMS = v
		}
		if p[6] > 0 {
			l.Brightness = int(p[6])
		}
	case EffectCycle:
		if v := int(p[5])<<8 | int(p[6]); v > 0 {
			l.PeriodMS = v
		}
		if p[7] > 0 {
			l.Brightness = int(p[7])
		}
	}
	return l
}

// Zones discovers LED zones and the effects of each (cached).
func (h *Headset) Zones() ([]Zone, error) {
	h.mu.Lock()
	cached := h.zones
	h.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	info, err := h.call(FeatLEDs, 0)
	if err != nil {
		return nil, err
	}
	caps := uint16(info[3])<<8 | uint16(info[4])
	var zones []Zone
	for z := 0; z < int(info[0]) && z < 8; z++ {
		zi, err := h.call(FeatLEDs, 1, byte(z), 0xFF, 0x00)
		if err != nil {
			return nil, err
		}
		zone := Zone{Index: byte(z), Location: uint16(zi[1])<<8 | uint16(zi[2]), slots: map[EffectID]byte{}}
		for e := 0; e < int(zi[3]) && e < 16; e++ {
			ei, err := h.call(FeatLEDs, 2, byte(z), byte(e), 0x00)
			if err != nil {
				return nil, err
			}
			id := EffectID(uint16(ei[2])<<8 | uint16(ei[3]))
			zone.slots[id] = byte(e)
			zone.Effects = append(zone.Effects, id)
		}
		zones = append(zones, zone)
	}
	h.mu.Lock()
	h.zones, h.ledCaps = zones, caps
	h.mu.Unlock()
	return zones, nil
}

func (h *Headset) zone(index byte) (Zone, error) {
	zones, err := h.Zones()
	if err != nil {
		return Zone{}, err
	}
	for _, z := range zones {
		if z.Index == index {
			return z, nil
		}
	}
	return Zone{}, i18n.Errorf("zone %d does not exist", index)
}

// LightsReadable reports whether getZoneEffect is supported.
func (h *Headset) LightsReadable() bool {
	if _, err := h.Zones(); err != nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ledCaps&0x01 != 0
}

// Light reads the current effect of a zone.
func (h *Headset) Light(index byte) (Light, error) {
	z, err := h.zone(index)
	if err != nil {
		return Light{}, err
	}
	r, err := h.call(FeatLEDs, 0xE, index)
	if err != nil {
		return Light{}, err
	}
	slot := r[1]
	for id, s := range z.slots {
		if s == slot {
			return decodeLight(id, r[2:12]), nil
		}
	}
	return Light{}, i18n.Errorf("zone %d: unknown effect #%d", index, slot)
}

// SetLight applies l to a zone. persist=true asks the headset to also store
// it in flash so it survives a power cycle; use false for live previews.
func (h *Headset) SetLight(index byte, l Light, persist bool) error {
	z, err := h.zone(index)
	if err != nil {
		return err
	}
	slot, ok := z.slots[l.Effect]
	if !ok {
		return i18n.Errorf("zone %s does not support the %s effect", z.Name(), l.Effect.Label())
	}
	p := encodeLight(l)
	params := append([]byte{index, slot}, p[:]...)
	if persist {
		params = append(params, 0x01)
	} else {
		params = append(params, 0x00)
	}
	_, err = h.call(FeatLEDs, 3, params...)
	return err
}
