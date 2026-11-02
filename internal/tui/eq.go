package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipearomani/g733-control/internal/g733"
	"github.com/felipearomani/g733-control/internal/i18n"
)

type eqTab struct {
	info  g733.EQInfo
	gains []int
	band  int
	err   error
}

func (t *eqTab) load(s snapshot) {
	t.info, t.err = s.eqInfo, s.eqErr
	t.gains = append([]int(nil), s.eq...)
	if len(t.gains) != len(t.info.Bands) {
		t.gains = make([]int, len(t.info.Bands))
	}
	t.band = clampInt(t.band, 0, max(len(t.gains)-1, 0))
}

func (t *eqTab) ready() bool { return t.err == nil && len(t.gains) > 0 }

// key handles one key press; it reports whether the gains changed.
func (t *eqTab) key(s string) bool {
	if !t.ready() {
		return false
	}
	before := append([]int(nil), t.gains...)
	n := len(t.gains)
	switch s {
	case "left", "h":
		t.band = (t.band - 1 + n) % n
	case "right", "l":
		t.band = (t.band + 1) % n
	case "up", "k":
		t.gains[t.band]++
	case "down", "j":
		t.gains[t.band]--
	case "shift+up", "K", "pgup":
		t.gains[t.band] += 3
	case "shift+down", "J", "pgdown":
		t.gains[t.band] -= 3
	case "0", "backspace":
		t.gains[t.band] = 0
	case "f":
		t.gains = g733.EQPresets[0].Gains(n)
	case "p", "P":
		cur := g733.MatchEQPreset(t.gains)
		dir := 1
		if s == "P" {
			dir = -1
		}
		np := len(g733.EQPresets)
		next := ((cur+dir)%np + np) % np
		if cur < 0 && dir < 0 {
			next = np - 1
		}
		t.gains = g733.EQPresets[next].Gains(n)
	}
	for i := range t.gains {
		t.gains[i] = clampInt(t.gains[i], t.info.Min, t.info.Max)
	}
	for i := range t.gains {
		if t.gains[i] != before[i] {
			return true
		}
	}
	return false
}

func (t eqTab) presetName() string {
	if i := g733.MatchEQPreset(t.gains); i >= 0 {
		return g733.EQPresets[i].Label()
	}
	return i18n.T("Custom")
}

func (t eqTab) view() string {
	if t.err != nil {
		return sErr.Render(i18n.T("Equalizer unavailable: ") + t.err.Error())
	}
	if !t.ready() {
		return sMuted.Render(i18n.T("Loading equalizer…"))
	}
	const col = 5
	axis := lipgloss.NewStyle().Foreground(colMuted).Width(5).Align(lipgloss.Right)
	barOn := lipgloss.NewStyle().Foreground(colAccent)
	barOff := lipgloss.NewStyle().Foreground(colBarDim)

	cell := func(b int, s string) string {
		st := barOff
		if b == t.band {
			st = barOn
		}
		return " " + st.Render(s) + " "
	}
	var lines []string
	for hi := t.info.Max; hi > 0; hi -= 2 { // rows covering (hi-2, hi]
		label := ""
		if hi == t.info.Max || hi == t.info.Max/2 {
			label = fmt.Sprintf("%+d", hi)
		}
		var b strings.Builder
		b.WriteString(axis.Render(label) + " ┤")
		for i, g := range t.gains {
			switch {
			case g >= hi:
				b.WriteString(cell(i, "███"))
			case g > hi-2:
				b.WriteString(cell(i, "▄▄▄"))
			default:
				b.WriteString(strings.Repeat(" ", col))
			}
		}
		lines = append(lines, b.String())
	}
	var zero strings.Builder
	zero.WriteString(axis.Render("0") + " ┼")
	for i := range t.gains {
		zero.WriteString(cell(i, "───"))
	}
	lines = append(lines, zero.String())
	for hi := 0; hi > t.info.Min; hi -= 2 { // rows covering [hi-2, hi)
		lo := hi - 2
		label := ""
		if lo == t.info.Min || lo == t.info.Min/2 {
			label = fmt.Sprintf("%+d", lo)
		}
		var b strings.Builder
		b.WriteString(axis.Render(label) + " ┤")
		for i, g := range t.gains {
			switch {
			case g <= lo:
				b.WriteString(cell(i, "███"))
			case g < hi:
				b.WriteString(cell(i, "▀▀▀"))
			default:
				b.WriteString(strings.Repeat(" ", col))
			}
		}
		lines = append(lines, b.String())
	}

	var freq, vals strings.Builder
	freq.WriteString(axis.Render("Hz") + "  ")
	vals.WriteString(axis.Render("dB") + "  ")
	center := lipgloss.NewStyle().Width(col).Align(lipgloss.Center)
	for i, hz := range t.info.Bands {
		st, vs := sMuted, sText
		if i == t.band {
			st, vs = sValueOn, sValueOn
		}
		freq.WriteString(center.Render(st.Render(g733.BandLabel(hz))))
		vals.WriteString(center.Render(vs.Render(fmt.Sprintf("%+d", t.gains[i]))))
	}
	lines = append(lines, freq.String(), vals.String(), "",
		"  "+sLabel.Render(i18n.T("Preset"))+sValueOn.Render("‹ "+t.presetName()+" ›"),
		"  "+sLabel.Render(i18n.T("Band"))+sText.Render(fmt.Sprintf("%s Hz  %+d dB", g733.BandLabel(t.info.Bands[t.band]), t.gains[t.band])))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
