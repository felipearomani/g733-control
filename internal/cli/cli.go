// Package cli implements the non-interactive subcommands, handy for scripts,
// shell aliases and global hotkeys (Raycast, Hammerspoon, skhd...).
package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/felipearomani/g733-control/internal/g733"
	"github.com/felipearomani/g733-control/internal/hidpp"
	"github.com/felipearomani/g733-control/internal/i18n"
)

const usageEN = `g733 — control the Logitech G733 over HID++ (no G HUB)

USAGE
  g733                         open the interactive interface
  g733 status                  summary: battery, lights, EQ, sidetone
  g733 info                    firmware, HID++ features, zones and bands
  g733 battery                 battery (%%, mV, charging)

  g733 lights                  current state of each zone
  g733 lights off              turn the lights off
  g733 lights static <color>   solid color      [--brightness 0-100]
  g733 lights breathe <color>  breathing        [--period ms] [--brightness 0-100]
  g733 lights cycle            color cycle      [--period ms] [--brightness 0-100]
      options: --zone logo|strips|all   --temp (do not save to flash)
      color: RRGGBB, #RGB or a name (%s)

  g733 sidetone [0-100]        read or set mic monitoring
  g733 eq                      show the bands
  g733 eq presets              list presets
  g733 eq preset <name>        apply a preset (a fragment is enough: "bass")
  g733 eq set <g1,g2,...>      gains in dB, one per band
  g733 eq flat                 reset every band
  g733 autooff [minutes]       read or set auto power-off (0 = never)

  g733 raw <feature> <fn> [bytes...]
                               raw HID++ call, e.g. g733 raw 1f20 0
  g733 lang [en|pt-br]         show or save the interface language
  g733 version

GLOBAL
  --lang en|pt-br              language for this run (also $G733_LANG)
`

const usagePT = `g733 — controle do Logitech G733 via HID++ (sem G HUB)

USO
  g733                         abre a interface interativa
  g733 status                  resumo: bateria, luzes, EQ, sidetone
  g733 info                    firmware, features HID++, zonas e bandas
  g733 battery                 bateria (%%, mV, carregando)

  g733 lights                  estado atual das zonas
  g733 lights off              apaga
  g733 lights static <cor>     cor fixa         [--brightness 0-100]
  g733 lights breathe <cor>    respirar         [--period ms] [--brightness 0-100]
  g733 lights cycle            ciclo de cores   [--period ms] [--brightness 0-100]
      opções: --zone logo|faixas|todas   --temp (não grava na flash)
      cor: RRGGBB, #RGB ou nome (%s)

  g733 sidetone [0-100]        lê ou define o retorno do microfone
  g733 eq                      mostra as bandas
  g733 eq presets              lista presets
  g733 eq preset <nome>        aplica preset (um trecho basta: "graves")
  g733 eq set <g1,g2,...>      ganhos em dB, um por banda
  g733 eq flat                 zera tudo
  g733 autooff [minutos]       lê ou define o auto-desligar (0 = nunca)

  g733 raw <feature> <fn> [bytes...]
                               chamada HID++ crua, ex.: g733 raw 1f20 0
  g733 lang [en|pt-br]         mostra ou salva o idioma da interface
  g733 version

GLOBAL
  --lang en|pt-br              idioma desta execução (também $G733_LANG)
`

// Run executes a subcommand and returns the process exit code.
func Run(args []string, version string) int {
	if len(args) == 0 {
		return 2
	}
	cmd, rest := strings.ToLower(args[0]), args[1:]
	switch cmd {
	case "-h", "--help", "help", "ajuda":
		usage := usageEN
		if i18n.Current() == i18n.PT {
			usage = usagePT
		}
		fmt.Printf(usage, colorNames())
		return 0
	case "-v", "--version", "version":
		fmt.Println("g733", version)
		return 0
	case "lang", "idioma", "language":
		return lang(rest)
	}

	run, ok := commands[cmd]
	if !ok {
		fmt.Fprintln(os.Stderr, "g733:", i18n.Tf("unknown command %q (see g733 help)", cmd))
		return 2
	}
	h, err := g733.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "g733:", i18n.T("dongle not found:"), err)
		return 1
	}
	defer h.Close()
	if err := run(h, parseArgs(rest)); err != nil {
		fmt.Fprintln(os.Stderr, "g733:", explain(err))
		return 1
	}
	return 0
}

var commands = map[string]func(*g733.Headset, args) error{
	"status":   status,
	"info":     info,
	"battery":  battery,
	"bateria":  battery,
	"lights":   lights,
	"luzes":    lights,
	"luz":      lights,
	"sidetone": sidetone,
	"eq":       eq,
	"autooff":  autoOff,
	"auto-off": autoOff,
	"raw":      raw,
}

func explain(err error) string {
	if errors.Is(err, hidpp.ErrTimeout) {
		return i18n.T("the headset did not answer — is it on and paired with the dongle?")
	}
	return err.Error()
}

func colorNames() string {
	var n []string
	for _, c := range g733.NamedColors {
		n = append(n, c.Name())
	}
	return strings.Join(n, ", ")
}

func lang(rest []string) int {
	if len(rest) == 0 {
		fmt.Println(i18n.Tf("Language: %s", i18n.Current().Name()))
		return 0
	}
	l, ok := i18n.Parse(rest[0])
	if !ok {
		fmt.Fprintln(os.Stderr, "g733:", i18n.Tf("unknown language %q (use en or pt-br)", rest[0]))
		return 2
	}
	i18n.Set(l)
	if err := i18n.Save(l); err != nil {
		fmt.Fprintln(os.Stderr, "g733:", err)
		return 1
	}
	fmt.Println(i18n.Tf("Language saved: %s", l.Name()))
	return 0
}

// args splits positionals from --flags (flags may appear anywhere).
type args struct {
	pos   []string
	flags map[string]string
}

func parseArgs(in []string) args {
	a := args{flags: map[string]string{}}
	for i := 0; i < len(in); i++ {
		s := in[i]
		if !strings.HasPrefix(s, "--") {
			a.pos = append(a.pos, s)
			continue
		}
		k := strings.TrimPrefix(s, "--")
		if key, val, ok := strings.Cut(k, "="); ok {
			a.flags[key] = val
			continue
		}
		if i+1 < len(in) && !strings.HasPrefix(in[i+1], "--") && k != "temp" {
			a.flags[k] = in[i+1]
			i++
			continue
		}
		a.flags[k] = "true"
	}
	return a
}

func (a args) intFlag(name string, def int) (int, error) {
	v, ok := a.flags[name]
	if !ok {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, i18n.Errorf("--%s must be a number, got %q", name, v)
	}
	return n, nil
}

// --- commands -------------------------------------------------------------------

func battery(h *g733.Headset, _ args) error {
	b, err := h.Battery()
	if err != nil {
		return err
	}
	fmt.Println(batteryText(b))
	return nil
}

func batteryText(b g733.Battery) string {
	state := i18n.T("discharging")
	switch {
	case !b.Active:
		state = i18n.T("headset inactive")
	case b.Charging:
		state = i18n.T("charging")
	}
	return i18n.Tf("%d%% (estimated)  %d mV  %s", b.Percent, b.MilliVolts, state)
}

func status(h *g733.Headset, _ args) error {
	if err := h.Ping(); err != nil {
		return err
	}
	name, _ := h.Name()
	fmt.Println(name)
	row := func(k, v string) { fmt.Printf("  %-16s %s\n", i18n.T(k), v) }
	if b, err := h.Battery(); err == nil {
		row("Battery", batteryText(b))
	}
	if zones, err := h.Zones(); err == nil {
		var parts []string
		for _, z := range zones {
			l, err := h.Light(z.Index)
			desc := l.String()
			if err != nil {
				desc = "?"
			}
			parts = append(parts, z.Name()+": "+desc)
		}
		row("Lights", strings.Join(parts, "  ·  "))
	}
	if gains, err := h.EQ(); err == nil {
		row("Equalizer", fmt.Sprintf("%s  %v", presetName(gains), gains))
	}
	if v, err := h.Sidetone(); err == nil {
		row("Sidetone", fmt.Sprintf("%d%%", v))
	}
	if v, err := h.AutoOff(); err == nil {
		row("Auto power-off", autoOffText(v))
	}
	return nil
}

func presetName(gains []int) string {
	if i := g733.MatchEQPreset(gains); i >= 0 {
		return g733.EQPresets[i].Label()
	}
	return i18n.T("Custom")
}

func autoOffText(min int) string {
	if min == 0 {
		return i18n.T("never")
	}
	return fmt.Sprintf("%d min", min)
}

func info(h *g733.Headset, _ args) error {
	if err := h.Ping(); err != nil {
		return err
	}
	name, _ := h.Name()
	fmt.Printf("%s  (046d:%04x)\n", name, h.ProductID)
	if fw, err := h.Firmware(); err == nil {
		for _, f := range fw {
			fmt.Println("  " + f.String())
		}
	}
	fmt.Println("\n" + i18n.T("HID++ features"))
	fs, err := h.Features()
	for _, f := range fs {
		fmt.Printf("  #%-2d 0x%04x v%-2d %s\n", f.Index, f.ID, f.Version, i18n.T(f.Name))
	}
	if err != nil {
		return err
	}
	if zones, err := h.Zones(); err == nil {
		fmt.Println("\n" + i18n.T("LED zones"))
		for _, z := range zones {
			var effs []string
			for _, e := range z.Effects {
				effs = append(effs, e.String())
			}
			fmt.Printf("  %d %-7s loc 0x%04x  %s\n", z.Index, z.Name(), z.Location, i18n.Tf("effects: %s", strings.Join(effs, ", ")))
		}
	}
	if eqi, err := h.EQInfo(); err == nil {
		var bands []string
		for _, b := range eqi.Bands {
			bands = append(bands, g733.BandLabel(b))
		}
		fmt.Printf("\n%s  %s\n", i18n.T("Equalizer"), i18n.Tf("%d bands (%s Hz), %+d to %+d dB", len(eqi.Bands), strings.Join(bands, " "), eqi.Min, eqi.Max))
	}
	return nil
}

func lights(h *g733.Headset, a args) error {
	zones, err := h.Zones()
	if err != nil {
		return err
	}
	if len(a.pos) == 0 {
		for _, z := range zones {
			l, err := h.Light(z.Index)
			if err != nil {
				fmt.Printf("%-7s ? (%v)\n", z.Name(), err)
				continue
			}
			fmt.Printf("%-7s %s\n", z.Name(), l.String())
		}
		return nil
	}

	l := g733.Light{Brightness: 100}
	switch strings.ToLower(a.pos[0]) {
	case "off", "desligar", "apagar":
		l.Effect = g733.EffectOff
	case "static", "fixa", "cor", "on", "solid":
		l.Effect = g733.EffectStatic
	case "breathe", "respirar":
		l.Effect = g733.EffectBreathe
	case "cycle", "ciclo", "rainbow":
		l.Effect = g733.EffectCycle
	default:
		return i18n.Errorf("unknown effect %q (off, static, breathe, cycle)", a.pos[0])
	}
	if l.Effect.UsesColor() {
		l.Color = g733.RGB{R: 0x00, G: 0xB6, B: 0xFF}
		if len(a.pos) > 1 {
			if l.Color, err = g733.ParseColor(a.pos[1]); err != nil {
				return err
			}
		}
	}
	if l.Brightness, err = a.intFlag("brightness", 100); err != nil {
		return err
	}
	if l.PeriodMS, err = a.intFlag("period", l.Effect.DefaultPeriod()); err != nil {
		return err
	}

	targets, err := pickZones(zones, a.flags["zone"])
	if err != nil {
		return err
	}
	persist := a.flags["temp"] == ""
	applied := 0
	for _, z := range targets {
		if !z.Supports(l.Effect) {
			fmt.Println(i18n.Tf("%-7s does not support %s, skipping", z.Name(), l.Effect.Label()))
			continue
		}
		if err := h.SetLight(z.Index, l, persist); err != nil {
			return fmt.Errorf("%s: %w", z.Name(), err)
		}
		applied++
		fmt.Printf("%-7s %s\n", z.Name(), l.String())
	}
	if applied == 0 {
		return errors.New(i18n.T("no zone was changed"))
	}
	return nil
}

// pickZones resolves --zone in either language (logo, strips/faixas, all/todas)
// or by index.
func pickZones(zones []g733.Zone, sel string) ([]g733.Zone, error) {
	sel = strings.ToLower(strings.TrimSpace(sel))
	switch sel {
	case "", "all", "todas", "ambas", "both":
		return zones, nil
	case "faixas", "faixa", "strip", "primary":
		sel = "strips"
	}
	for _, z := range zones {
		if z.ID() == sel || strconv.Itoa(int(z.Index)) == sel {
			return []g733.Zone{z}, nil
		}
	}
	return nil, i18n.Errorf("zone %q does not exist (logo, strips, all)", sel)
}

func sidetone(h *g733.Headset, a args) error {
	if len(a.pos) == 0 {
		v, err := h.Sidetone()
		if err != nil {
			return err
		}
		fmt.Printf("%d%%\n", v)
		return nil
	}
	v, err := strconv.Atoi(strings.TrimSuffix(a.pos[0], "%"))
	if err != nil || v < 0 || v > 100 {
		return i18n.Errorf("sidetone must be 0-100, got %q", a.pos[0])
	}
	if err := h.SetSidetone(v); err != nil {
		return err
	}
	fmt.Printf("sidetone %d%%\n", v)
	return nil
}

func eq(h *g733.Headset, a args) error {
	info, err := h.EQInfo()
	if err != nil {
		return err
	}
	sub := ""
	if len(a.pos) > 0 {
		sub = strings.ToLower(a.pos[0])
	}
	var gains []int
	switch sub {
	case "":
		cur, err := h.EQ()
		if err != nil {
			return err
		}
		printEQ(info, cur)
		return nil
	case "presets":
		for _, p := range g733.EQPresets {
			fmt.Printf("  %-20s %v\n", p.Label(), p.Gains(len(info.Bands)))
		}
		return nil
	case "flat", "reset":
		gains = make([]int, len(info.Bands))
	case "preset":
		name := strings.Join(a.pos[1:], " ")
		if name == "" {
			return errors.New(i18n.T("name the preset (g733 eq presets)"))
		}
		p, ok := g733.FindEQPreset(name)
		if !ok {
			return i18n.Errorf("preset %q does not exist (g733 eq presets)", name)
		}
		gains = p.Gains(len(info.Bands))
	case "set":
		raw := strings.Join(a.pos[1:], ",")
		for _, f := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' }) {
			g, err := strconv.Atoi(f)
			if err != nil {
				return i18n.Errorf("invalid gain %q", f)
			}
			gains = append(gains, g)
		}
		if len(gains) != len(info.Bands) {
			return i18n.Errorf("need %d gains (one per band), got %d", len(info.Bands), len(gains))
		}
		for _, g := range gains {
			if g < info.Min || g > info.Max {
				return i18n.Errorf("gain %d outside %+d..%+d dB", g, info.Min, info.Max)
			}
		}
	default:
		return i18n.Errorf("unknown eq subcommand %q", sub)
	}
	if err := h.SetEQ(gains); err != nil {
		return err
	}
	printEQ(info, gains)
	return nil
}

func printEQ(info g733.EQInfo, gains []int) {
	fmt.Println(i18n.T("Preset") + ": " + presetName(gains))
	for i, hz := range info.Bands {
		g := 0
		if i < len(gains) {
			g = gains[i]
		}
		pos := strings.Repeat("█", max(g, 0))
		neg := strings.Repeat("█", max(-g, 0))
		fmt.Printf("  %6s Hz  %+3d dB  %12s|%-12s\n", g733.BandLabel(hz), g, neg, pos)
	}
}

func autoOff(h *g733.Headset, a args) error {
	if len(a.pos) == 0 {
		v, err := h.AutoOff()
		if err != nil {
			return err
		}
		fmt.Println(autoOffText(v))
		return nil
	}
	v, err := strconv.Atoi(a.pos[0])
	if err != nil || v < 0 || v > 255 {
		return i18n.Errorf("minutes must be 0-255, got %q", a.pos[0])
	}
	if err := h.SetAutoOff(v); err != nil {
		return err
	}
	fmt.Println(i18n.Tf("auto power-off: %s", autoOffText(v)))
	return nil
}

func raw(h *g733.Headset, a args) error {
	if len(a.pos) < 2 {
		return errors.New(i18n.T("usage: g733 raw <feature-hex> <function> [hex bytes...]"))
	}
	fid, err := strconv.ParseUint(strings.TrimPrefix(a.pos[0], "0x"), 16, 16)
	if err != nil {
		return i18n.Errorf("invalid feature %q", a.pos[0])
	}
	fn, err := strconv.ParseUint(a.pos[1], 16, 4)
	if err != nil {
		return i18n.Errorf("invalid function %q (0-f)", a.pos[1])
	}
	var params []byte
	for _, p := range a.pos[2:] {
		b, err := strconv.ParseUint(strings.TrimPrefix(p, "0x"), 16, 8)
		if err != nil {
			return i18n.Errorf("invalid byte %q", p)
		}
		params = append(params, byte(b))
	}
	r, err := h.Raw(uint16(fid), byte(fn), params...)
	if err != nil {
		return err
	}
	fmt.Printf("% x\n", r)
	return nil
}
