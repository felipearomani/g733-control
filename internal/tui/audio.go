package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipearomani/g733-control/internal/g733"
	"github.com/felipearomani/g733-control/internal/i18n"
	colorful "github.com/lucasb-eyer/go-colorful"
)

var autoOffChoices = []int{0, 5, 10, 15, 20, 30, 45, 60, 90, 120, 180, 240}

type audioTab struct {
	sidetone    int
	sidetoneErr error
	autoOff     int
	autoOffErr  error
	focus       int // 0 sidetone, 1 auto-off
}

func (t *audioTab) load(s snapshot) {
	t.sidetone, t.sidetoneErr = s.sidetone, s.sidetoneErr
	t.autoOff, t.autoOffErr = s.autoOff, s.autoOffErr
}

// key returns the kind of setting that changed ("" if none).
func (t *audioTab) key(s string) string {
	switch s {
	case "up", "k", "down", "j":
		t.focus = 1 - t.focus
		return ""
	}
	dir := 0
	big := false
	switch s {
	case "left", "h":
		dir = -1
	case "right", "l":
		dir = 1
	case "shift+left", "H":
		dir, big = -1, true
	case "shift+right", "L":
		dir, big = 1, true
	}
	if dir == 0 {
		return ""
	}
	if t.focus == 0 {
		if t.sidetoneErr != nil {
			return ""
		}
		step := 5
		if big {
			step = 20
		}
		next := clampInt(t.sidetone+dir*step, 0, 100)
		if next == t.sidetone {
			return ""
		}
		t.sidetone = next
		return kindSidetone
	}
	if t.autoOffErr != nil {
		return ""
	}
	next := nextChoice(autoOffChoices, t.autoOff, dir)
	if next == t.autoOff {
		return ""
	}
	t.autoOff = next
	return kindAutoOff
}

// nextChoice moves to the neighbouring option, even if cur is not listed.
func nextChoice(choices []int, cur, dir int) int {
	if dir > 0 {
		for _, c := range choices {
			if c > cur {
				return c
			}
		}
		return cur
	}
	for i := len(choices) - 1; i >= 0; i-- {
		if choices[i] < cur {
			return choices[i]
		}
	}
	return cur
}

func autoOffLabel(min int) string {
	if min == 0 {
		return i18n.T("Never")
	}
	if min >= 60 && min%60 == 0 {
		return fmt.Sprintf("%d h", min/60)
	}
	return fmt.Sprintf("%d min", min)
}

func (t audioTab) view(width int) string {
	sw := clampInt(width-32, 16, 40)
	var sidetone, autoOff string
	if t.sidetoneErr != nil {
		sidetone = sErr.Render(i18n.T("unavailable: ") + t.sidetoneErr.Error())
	} else {
		sidetone = slider(sw, float64(t.sidetone)/100, t.focus == 0, func(x float64) colorful.Color {
			return colorful.Color{R: 0.2 + 0.8*x*accentColorful.R, G: 0.2 + 0.8*x*accentColorful.G, B: 0.2 + 0.8*x*accentColorful.B}
		}) + sText.Render(fmt.Sprintf("  %3d%%", t.sidetone))
	}
	if t.autoOffErr != nil {
		autoOff = sErr.Render(i18n.T("unavailable: ") + t.autoOffErr.Error())
	} else {
		autoOff = selector(autoOffLabel(t.autoOff), t.focus == 1)
	}
	hint := func(s string) string { return "  " + sLabel.Render("") + sMuted.Render(i18n.T(s)) }
	return lipgloss.JoinVertical(lipgloss.Left,
		row(i18n.T("Sidetone"), t.focus == 0, sidetone),
		hint("your own voice in the headset (0 = off)"),
		"",
		row(i18n.T("Auto power-off"), t.focus == 1, autoOff),
		hint("turns off by itself after this idle time"),
	)
}

func infoView(m Model) string {
	kv := func(k, v string) string { return "  " + sLabel.Render(i18n.T(k)) + sText.Render(v) }
	var fw []string
	for _, f := range m.firmware {
		fw = append(fw, f.String())
	}
	fwText := "?"
	if len(fw) > 0 {
		fwText = fw[0]
		for _, f := range fw[1:] {
			fwText += " · " + f
		}
	}
	bat := "?"
	if m.hasBattery {
		state := i18n.T("discharging")
		if m.battery.Charging {
			state = i18n.T("charging")
		}
		bat = i18n.Tf("%d%% (estimated)  ·  %d mV  ·  %s", m.battery.Percent, m.battery.MilliVolts, state)
	}
	var zones string
	for i, z := range m.lights.zones {
		if i > 0 {
			zones += ", "
		}
		zones += z.Name()
	}
	eq := "?"
	if m.eq.ready() && len(m.eq.info.Bands) > 0 {
		b := m.eq.info.Bands
		eq = i18n.Tf("%d bands, %s–%s Hz, %+d to %+d dB", len(b), g733.BandLabel(b[0]), g733.BandLabel(b[len(b)-1]), m.eq.info.Min, m.eq.info.Max)
	}
	lines := []string{
		kv("Device", fmt.Sprintf("%s  (046d:%04x)", m.name, m.productID)),
		kv("Firmware", fwText),
		kv("Battery", bat),
		kv("LED zones", zones),
		kv("Equalizer", eq),
		"",
		"  " + sTitle.Render(i18n.T("HID++ features")),
		"  " + sMuted.Render(fmt.Sprintf("%-4s %-8s %-4s %s", "#", "ID", "ver", i18n.T("name"))),
	}
	for _, f := range m.features {
		name := i18n.T(f.Name)
		if name == "" {
			name = "?"
		}
		lines = append(lines, "  "+sText.Render(fmt.Sprintf("%-4d 0x%04x   v%-3d %s", f.Index, f.ID, f.Version, name)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
