package tui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipearomani/g733-control/internal/g733"
	colorful "github.com/lucasb-eyer/go-colorful"
)

var (
	colAccent = lipgloss.Color("#00B6FF")
	colMuted  = lipgloss.AdaptiveColor{Light: "#8C8FA1", Dark: "#6C7086"}
	colText   = lipgloss.AdaptiveColor{Light: "#4C4F69", Dark: "#CDD6F4"}
	colOK     = lipgloss.Color("#40C057")
	colWarn   = lipgloss.Color("#FAB005")
	colErr    = lipgloss.Color("#FA5252")
	colBorder = lipgloss.AdaptiveColor{Light: "#BCC0CC", Dark: "#45475A"}
	colBarDim = lipgloss.AdaptiveColor{Light: "#7FA7C9", Dark: "#3B6E91"}

	sTitle   = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	sMuted   = lipgloss.NewStyle().Foreground(colMuted)
	sText    = lipgloss.NewStyle().Foreground(colText)
	sLabel   = lipgloss.NewStyle().Foreground(colMuted).Width(16)
	sLabelOn = lipgloss.NewStyle().Foreground(colAccent).Bold(true).Width(16)
	sValueOn = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	sOK      = lipgloss.NewStyle().Foreground(colOK)
	sWarn    = lipgloss.NewStyle().Foreground(colWarn)
	sErr     = lipgloss.NewStyle().Foreground(colErr)
	sKey     = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	sTabOn   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#0B0E14")).Background(colAccent).Padding(0, 2)
	sTabOff  = lipgloss.NewStyle().Foreground(colMuted).Padding(0, 2)
	sPanel   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colBorder).Padding(1, 2)
)

func toColorful(c g733.RGB) colorful.Color {
	return colorful.Color{R: float64(c.R) / 255, G: float64(c.G) / 255, B: float64(c.B) / 255}
}

func fromColorful(c colorful.Color) g733.RGB {
	r, g, b := c.Clamped().RGB255()
	return g733.RGB{R: r, G: g, B: b}
}

// slider renders a bar of width cells with a knob at frac (0..1); grad
// colors each cell so the bar previews what the value does.
func slider(width int, frac float64, focused bool, grad func(t float64) colorful.Color) string {
	width = max(width, 4)
	if math.IsNaN(frac) {
		frac = 0
	}
	frac = math.Max(0, math.Min(1, frac))
	pos := int(math.Round(frac * float64(width-1)))
	knob := lipgloss.NewStyle().Foreground(colText)
	line := "─"
	if focused {
		knob = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
		line = "━"
	}
	var b strings.Builder
	for i := 0; i < width; i++ {
		if i == pos {
			b.WriteString(knob.Render("●"))
			continue
		}
		t := float64(i) / float64(width-1)
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(grad(t).Clamped().Hex())).Render(line))
	}
	return b.String()
}

func solid(c colorful.Color) func(float64) colorful.Color {
	return func(float64) colorful.Color { return c }
}

var accentColorful, _ = colorful.Hex("#00B6FF")

// selector renders a cyclable value.
func selector(text string, focused bool) string {
	if focused {
		return sValueOn.Render("‹ " + text + " ›")
	}
	return sText.Render("  " + text)
}

// row renders "▸ Label   content" with the focus marker.
func row(label string, focused bool, content string) string {
	if focused {
		return sKey.Render("▸ ") + sLabelOn.Render(label) + content
	}
	return "  " + sLabel.Render(label) + content
}

// swatch is a block of terminal cells painted with c.
func swatch(c g733.RGB, width int, text string) string {
	fg := lipgloss.Color("#000000")
	if luminance(c) < 0.45 {
		fg = lipgloss.Color("#FFFFFF")
	}
	pad := max(width-lipgloss.Width(text), 0)
	left := pad / 2
	body := strings.Repeat(" ", left) + text + strings.Repeat(" ", pad-left)
	return lipgloss.NewStyle().Background(lipgloss.Color(c.Hex())).Foreground(fg).Render(body)
}

func luminance(c g733.RGB) float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}

func batteryColor(pct int) lipgloss.TerminalColor {
	switch {
	case pct > 50:
		return colOK
	case pct > 20:
		return colWarn
	}
	return colErr
}

func batteryBar(pct, width int) string {
	filled := (pct*width + 50) / 100
	filled = max(0, min(width, filled))
	return lipgloss.NewStyle().Foreground(batteryColor(pct)).Render(strings.Repeat("▮", filled)) +
		sMuted.Render(strings.Repeat("▯", width-filled))
}
