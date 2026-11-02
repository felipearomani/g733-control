package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/felipearomani/g733-control/internal/g733"
	"github.com/felipearomani/g733-control/internal/i18n"
	colorful "github.com/lucasb-eyer/go-colorful"
)

type lightField int

const (
	lfZone lightField = iota
	lfEffect
	lfPalette
	lfHue
	lfSat
	lfRed
	lfGreen
	lfBlue
	lfHex
	lfBright
	lfPeriod
)

// lightLabels are English keys, translated at render time.
var lightLabels = map[lightField]string{
	lfZone: "Zone", lfEffect: "Effect", lfPalette: "Palette", lfHue: "Hue",
	lfSat: "Saturation", lfRed: "Red", lfGreen: "Green", lfBlue: "Blue",
	lfHex: "Hex", lfBright: "Brightness", lfPeriod: "Period",
}

const (
	minPeriod = 500
	maxPeriod = 30000
)

var uiEffects = []g733.EffectID{g733.EffectOff, g733.EffectStatic, g733.EffectBreathe, g733.EffectCycle}

type lightsTab struct {
	zones    []g733.Zone
	state    map[byte]g733.Light // last known state of each zone on the headset
	readable bool
	err      error

	target int        // 0 = every zone, i = zones[i-1]
	edit   g733.Light // what the editor shows / sends
	hue    float64    // kept apart so it survives grays
	sat    float64
	val    float64
	focus  lightField
	hex    textinput.Model
	lastOn g733.Light // restored by the on/off toggle
}

func defaultLight() g733.Light {
	return g733.Light{Effect: g733.EffectStatic, Color: g733.RGB{R: 0x00, G: 0xB6, B: 0xFF}, Brightness: 100, PeriodMS: 4000}
}

func newLightsTab() lightsTab {
	ti := textinput.New()
	ti.Prompt = "#"
	ti.CharLimit = 6
	ti.Width = 7
	ti.Placeholder = "00B6FF"
	ti.Cursor.SetMode(cursor.CursorStatic)
	t := lightsTab{state: map[byte]g733.Light{}, hex: ti, edit: defaultLight(), lastOn: defaultLight(), focus: lfEffect}
	t.syncFromRGB()
	return t
}

func (t *lightsTab) load(s snapshot) {
	t.zones, t.readable, t.err = s.zones, s.lightsReadable, s.lightsErr
	t.state = map[byte]g733.Light{}
	for k, v := range s.lights {
		t.state[k] = v
	}
	for _, z := range t.zones {
		if l, ok := t.state[z.Index]; ok && l.Effect != g733.EffectOff && l.Effect.UsesColor() {
			t.lastOn = l
			break
		}
	}
	if t.target > len(t.zones) {
		t.target = 0
	}
	t.loadEditor()
}

// loadEditor shows the known state of the selected zone (first zone for "all").
func (t *lightsTab) loadEditor() {
	if len(t.zones) == 0 {
		return
	}
	zi := t.zones[0].Index
	if t.target > 0 {
		zi = t.zones[t.target-1].Index
	}
	if l, ok := t.state[zi]; ok {
		if !l.Effect.UsesColor() {
			l.Color = t.lastOn.Color // keep a sensible color to switch back to
		}
		t.edit = l
	}
	if t.edit.PeriodMS <= 0 {
		t.edit.PeriodMS = t.edit.Effect.DefaultPeriod()
	}
	t.syncFromRGB()
	t.ensureFocus()
}

func (t *lightsTab) fields() []lightField {
	f := []lightField{lfZone, lfEffect}
	switch t.edit.Effect {
	case g733.EffectStatic:
		f = append(f, lfPalette, lfHue, lfSat, lfRed, lfGreen, lfBlue, lfHex, lfBright)
	case g733.EffectBreathe, g733.EffectPulse:
		f = append(f, lfPalette, lfHue, lfSat, lfRed, lfGreen, lfBlue, lfHex, lfBright, lfPeriod)
	case g733.EffectCycle:
		f = append(f, lfBright, lfPeriod)
	}
	return f
}

func (t *lightsTab) ensureFocus() {
	for _, f := range t.fields() {
		if f == t.focus {
			return
		}
	}
	t.setFocus(lfEffect)
}

func (t *lightsTab) setFocus(f lightField) {
	t.focus = f
	if f == lfHex {
		t.hex.SetValue(strings.TrimPrefix(t.edit.Color.Hex(), "#"))
		t.hex.CursorEnd()
		t.hex.Focus()
	} else {
		t.hex.Blur()
	}
}

func (t *lightsTab) typing() bool { return t.focus == lfHex }

// effects lists the effects available for the current target.
func (t *lightsTab) effects() []g733.EffectID {
	var out []g733.EffectID
	for _, e := range uiEffects {
		for i, z := range t.zones {
			if (t.target == 0 || t.target == i+1) && z.Supports(e) {
				out = append(out, e)
				break
			}
		}
	}
	if len(out) == 0 {
		out = []g733.EffectID{g733.EffectOff}
	}
	return out
}

// targetZones are the zone indices that receive the current edit.
func (t *lightsTab) targetZones() []byte {
	var out []byte
	for i, z := range t.zones {
		if (t.target == 0 || t.target == i+1) && z.Supports(t.edit.Effect) {
			out = append(out, z.Index)
		}
	}
	return out
}

func (t *lightsTab) targetName() string {
	if t.target == 0 || t.target > len(t.zones) {
		return i18n.T("All")
	}
	return t.zones[t.target-1].Name()
}

func (t *lightsTab) syncFromRGB() {
	h, s, v := toColorful(t.edit.Color).Hsv()
	if s > 0.001 && !math.IsNaN(h) {
		t.hue = h
	}
	t.sat, t.val = s, v
	if !t.hex.Focused() {
		t.hex.SetValue(strings.TrimPrefix(t.edit.Color.Hex(), "#"))
	}
}

func (t *lightsTab) syncFromHSV() {
	if t.val < 0.05 {
		t.val = 1 // editing hue/saturation of black would do nothing
	}
	t.edit.Color = fromColorful(colorful.Hsv(t.hue, t.sat, t.val))
	t.hex.SetValue(strings.TrimPrefix(t.edit.Color.Hex(), "#"))
}

func (t *lightsTab) paletteIndex() int {
	for i, nc := range g733.NamedColors {
		if nc.Color == t.edit.Color {
			return i
		}
	}
	return -1
}

// toggle switches between off and the last lit state.
func (t *lightsTab) toggle() {
	if t.edit.Effect != g733.EffectOff {
		t.lastOn = t.edit
		t.edit.Effect = g733.EffectOff
	} else {
		t.edit = t.lastOn
		if t.edit.Effect == g733.EffectOff {
			t.edit = defaultLight()
		}
	}
	t.syncFromRGB()
	t.ensureFocus()
}

// adjust changes the focused field by one step; it reports whether the
// light itself changed (zone selection only changes the editor).
func (t *lightsTab) adjust(dir int, big bool) bool {
	before := t.edit
	step := func(small, large int) int {
		if big {
			return dir * large
		}
		return dir * small
	}
	switch t.focus {
	case lfZone:
		n := len(t.zones) + 1
		t.target = ((t.target+dir)%n + n) % n
		t.loadEditor()
		return false
	case lfEffect:
		effs := t.effects()
		i := 0
		for j, e := range effs {
			if e == t.edit.Effect {
				i = j
			}
		}
		next := effs[((i+dir)%len(effs)+len(effs))%len(effs)]
		if next != t.edit.Effect {
			t.edit.Effect = next
			t.edit.PeriodMS = next.DefaultPeriod()
			if next.UsesColor() && t.edit.Color == (g733.RGB{}) {
				t.edit.Color = t.lastOn.Color
			}
			if t.edit.Brightness <= 0 {
				t.edit.Brightness = 100
			}
			t.syncFromRGB()
		}
		t.ensureFocus()
	case lfPalette:
		n := len(g733.NamedColors)
		i := t.paletteIndex()
		switch {
		case i < 0 && dir > 0:
			i = 0
		case i < 0:
			i = n - 1
		default:
			i = ((i+dir)%n + n) % n
		}
		t.edit.Color = g733.NamedColors[i].Color
		t.syncFromRGB()
	case lfHue:
		t.hue = math.Mod(t.hue+float64(step(5, 30))+360, 360)
		if t.sat < 0.05 {
			t.sat = 1
		}
		t.syncFromHSV()
	case lfSat:
		t.sat = math.Max(0, math.Min(1, t.sat+float64(step(5, 20))/100))
		t.syncFromHSV()
	case lfRed:
		t.edit.Color.R = uint8(clampInt(int(t.edit.Color.R)+step(5, 32), 0, 255))
		t.syncFromRGB()
	case lfGreen:
		t.edit.Color.G = uint8(clampInt(int(t.edit.Color.G)+step(5, 32), 0, 255))
		t.syncFromRGB()
	case lfBlue:
		t.edit.Color.B = uint8(clampInt(int(t.edit.Color.B)+step(5, 32), 0, 255))
		t.syncFromRGB()
	case lfBright:
		t.edit.Brightness = clampInt(t.edit.Brightness+step(5, 20), 0, 100)
	case lfPeriod:
		t.edit.PeriodMS = clampInt(t.edit.PeriodMS+step(250, 1000), minPeriod, maxPeriod)
	}
	return t.edit != before
}

func clampInt(v, lo, hi int) int { return max(lo, min(hi, v)) }

// --- view ---------------------------------------------------------------------

func (t lightsTab) view(width int, now time.Time) string {
	if t.err != nil {
		return sErr.Render(i18n.T("Lights unavailable: ") + t.err.Error())
	}
	if len(t.zones) == 0 {
		return sMuted.Render(i18n.T("This headset exposed no LED zones."))
	}
	sw := clampInt(width-32, 16, 40)
	c := t.edit.Color
	cf := toColorful(c)
	var rows []string
	for _, f := range t.fields() {
		on := f == t.focus
		var content string
		switch f {
		case lfZone:
			content = selector(t.targetName(), on)
		case lfEffect:
			content = selector(t.edit.Effect.Label(), on)
		case lfPalette:
			content = t.paletteView(on)
		case lfHue:
			content = slider(sw, t.hue/360, on, func(x float64) colorful.Color { return colorful.Hsv(x*360, 1, 1) }) +
				sText.Render(fmt.Sprintf("  %3.0f°", t.hue))
		case lfSat:
			content = slider(sw, t.sat, on, func(x float64) colorful.Color { return colorful.Hsv(t.hue, x, 1) }) +
				sText.Render(fmt.Sprintf("  %3.0f%%", t.sat*100))
		case lfRed:
			content = slider(sw, float64(c.R)/255, on, func(x float64) colorful.Color { return colorful.Color{R: x, G: cf.G, B: cf.B} }) +
				sText.Render(fmt.Sprintf("  %3d", c.R))
		case lfGreen:
			content = slider(sw, float64(c.G)/255, on, func(x float64) colorful.Color { return colorful.Color{R: cf.R, G: x, B: cf.B} }) +
				sText.Render(fmt.Sprintf("  %3d", c.G))
		case lfBlue:
			content = slider(sw, float64(c.B)/255, on, func(x float64) colorful.Color { return colorful.Color{R: cf.R, G: cf.G, B: x} }) +
				sText.Render(fmt.Sprintf("  %3d", c.B))
		case lfHex:
			if on {
				content = t.hex.View() + sMuted.Render("  "+i18n.T("type RRGGBB"))
			} else {
				content = sText.Render(c.Hex())
			}
		case lfBright:
			base := cf
			if !t.edit.Effect.UsesColor() {
				base = colorful.Color{R: 1, G: 1, B: 1}
			}
			content = slider(sw, float64(t.edit.Brightness)/100, on, func(x float64) colorful.Color {
				return colorful.Color{R: base.R * x, G: base.G * x, B: base.B * x}
			}) + sText.Render(fmt.Sprintf("  %3d%%", t.edit.Brightness))
		case lfPeriod:
			frac := float64(t.edit.PeriodMS-minPeriod) / float64(maxPeriod-minPeriod)
			content = slider(sw, frac, on, solid(accentColorful)) +
				sText.Render(fmt.Sprintf("  %4.1f s", float64(t.edit.PeriodMS)/1000))
		}
		rows = append(rows, row(i18n.T(lightLabels[f]), on, content))
		if f == lfEffect {
			rows = append(rows, "")
		}
	}
	rows = append(rows, "", "  "+sLabel.Render(i18n.T("Preview"))+t.preview(now), "", t.stateLine())
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (t lightsTab) paletteView(focused bool) string {
	sel := t.paletteIndex()
	var b strings.Builder
	for i, nc := range g733.NamedColors {
		mark := ""
		if i == sel {
			mark = "●"
		}
		b.WriteString(swatch(nc.Color, 3, mark))
		if i < len(g733.NamedColors)-1 {
			b.WriteString(" ")
		}
	}
	name := i18n.T("custom")
	if sel >= 0 {
		name = g733.NamedColors[sel].Name()
	}
	st := sMuted
	if focused {
		st = sValueOn
	}
	return b.String() + "  " + st.Render(name)
}

// preview animates the effect in the terminal so changes can be judged
// before (or while) they reach the headset.
func (t lightsTab) preview(now time.Time) string {
	period := float64(max(t.edit.PeriodMS, minPeriod))
	phase := math.Mod(float64(now.UnixMilli()), period) / period
	bright := float64(t.edit.Brightness) / 100
	var c g733.RGB
	label := ""
	switch t.edit.Effect {
	case g733.EffectOff:
		label = i18n.T("off")
	case g733.EffectStatic:
		c = t.edit.Color.Scale(t.edit.Brightness)
	case g733.EffectBreathe, g733.EffectPulse:
		k := (1 - math.Cos(2*math.Pi*phase)) / 2
		c = t.edit.Color.Scale(int(math.Round(k * bright * 100)))
	case g733.EffectCycle:
		c = fromColorful(colorful.Hsv(phase*360, 1, bright))
	}
	return swatch(c, 18, label)
}

func (t lightsTab) stateLine() string {
	var parts []string
	for _, z := range t.zones {
		desc := "?"
		if l, ok := t.state[z.Index]; ok {
			desc = l.String()
		} else if !t.readable {
			desc = i18n.T("unreadable")
		}
		parts = append(parts, sText.Render(z.Name())+sMuted.Render(": "+desc))
	}
	return "  " + sLabel.Render(i18n.T("On headset")) + strings.Join(parts, sMuted.Render("  ·  "))
}
