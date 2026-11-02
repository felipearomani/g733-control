package g733

import (
	"fmt"
	"strings"

	"github.com/felipearomani/g733-control/internal/i18n"
)

// EQInfo describes the equalizer bands (0x8310 getInfo + getFrequencies).
type EQInfo struct {
	Bands []int // center frequency of each band, Hz
	Min   int   // dB
	Max   int   // dB
}

// BandLabel formats a band frequency compactly (32, 125, 1k, 16k).
func BandLabel(hz int) string {
	if hz >= 1000 {
		if hz%1000 == 0 {
			return fmt.Sprintf("%dk", hz/1000)
		}
		return fmt.Sprintf("%.1fk", float64(hz)/1000)
	}
	return fmt.Sprintf("%d", hz)
}

func (h *Headset) EQInfo() (EQInfo, error) {
	h.mu.Lock()
	cached := h.eqInfo
	h.mu.Unlock()
	if cached != nil {
		return *cached, nil
	}
	r, err := h.call(FeatEQ, 0)
	if err != nil {
		return EQInfo{}, err
	}
	info := parseEQInfo(r)
	count := int(r[0])
	for start := 0; start < count; start += 7 {
		f, err := h.call(FeatEQ, 1, byte(start))
		if err != nil {
			return EQInfo{}, err
		}
		info.Bands = append(info.Bands, parseEQFrequencies(f, count-start)...)
	}
	if len(info.Bands) == 0 || len(info.Bands) > 15 {
		return EQInfo{}, i18n.Errorf("equalizer reports %d bands", len(info.Bands))
	}
	h.mu.Lock()
	h.eqInfo = &info
	h.mu.Unlock()
	return info, nil
}

func parseEQInfo(r []byte) EQInfo {
	if len(r) < 5 {
		return EQInfo{Min: -12, Max: 12}
	}
	rng := int(r[1])
	lo, hi := int(int8(r[3])), int(int8(r[4]))
	if lo == 0 {
		lo = -rng
	}
	if hi == 0 {
		hi = rng
	}
	return EQInfo{Min: lo, Max: hi}
}

// parseEQFrequencies reads up to 7 big-endian Hz values after the start byte.
func parseEQFrequencies(f []byte, remaining int) []int {
	var out []int
	for b := 0; b < 7 && b < remaining; b++ {
		i := 1 + 2*b
		if i+1 >= len(f) {
			break
		}
		out = append(out, int(f[i])<<8|int(f[i+1]))
	}
	return out
}

// EQ returns the current gain of every band in dB.
func (h *Headset) EQ() ([]int, error) {
	info, err := h.EQInfo()
	if err != nil {
		return nil, err
	}
	r, err := h.call(FeatEQ, 2, 0x00)
	if err != nil {
		return nil, err
	}
	gains := make([]int, len(info.Bands))
	for i := range gains {
		if i < len(r) {
			gains[i] = int(int8(r[i]))
		}
	}
	return gains, nil
}

// SetEQ writes every band gain in dB (clamped to the device range).
func (h *Headset) SetEQ(gains []int) error {
	info, err := h.EQInfo()
	if err != nil {
		return err
	}
	if len(gains) != len(info.Bands) {
		return i18n.Errorf("equalizer needs %d gains, got %d", len(info.Bands), len(gains))
	}
	params := []byte{0x02}
	for _, g := range gains {
		params = append(params, byte(int8(clamp(g, info.Min, info.Max))))
	}
	_, err = h.call(FeatEQ, 3, params...)
	return err
}

// EQPreset is a named 10-band curve; Gains adapts it to other band counts.
type EQPreset struct {
	Name  string // English name, also the translation key
	Curve []int
}

// Label is the preset name in the active language.
func (p EQPreset) Label() string { return i18n.T(p.Name) }

var EQPresets = []EQPreset{
	{"Flat", []int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
	{"Bass Boost", []int{7, 6, 4, 2, 0, 0, 0, 0, 0, 0}},
	{"V-Shape", []int{5, 4, 2, 0, -2, -2, 0, 2, 4, 5}},
	{"Voice / Podcast", []int{-3, -2, 0, 1, 3, 4, 4, 2, 0, -1}},
	{"FPS / Footsteps", []int{-4, -3, -2, -1, 0, 2, 4, 5, 4, 2}},
	{"Treble Boost", []int{0, 0, 0, 0, 0, 1, 2, 4, 5, 6}},
	{"Cinema", []int{5, 4, 2, 0, -1, 0, 1, 2, 3, 3}},
	{"Warm Music", []int{3, 3, 2, 1, 0, -1, -1, 0, 1, 1}},
}

func (p EQPreset) Gains(bands int) []int {
	out := make([]int, bands)
	if len(p.Curve) == 0 || bands <= 0 {
		return out
	}
	for i := range out {
		out[i] = p.Curve[i*len(p.Curve)/bands]
	}
	return out
}

// FindEQPreset matches a name fragment in English or Portuguese.
func FindEQPreset(name string) (EQPreset, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return EQPreset{}, false
	}
	for _, p := range EQPresets {
		ptName := strings.ToLower(i18n.In(i18n.PT, p.Name))
		if strings.Contains(strings.ToLower(p.Name), name) || strings.Contains(ptName, name) {
			return p, true
		}
	}
	return EQPreset{}, false
}

// MatchEQPreset returns the index of the preset equal to gains, or -1.
func MatchEQPreset(gains []int) int {
	for i, p := range EQPresets {
		g := p.Gains(len(gains))
		same := len(g) == len(gains)
		for j := 0; same && j < len(g); j++ {
			same = g[j] == gains[j]
		}
		if same {
			return i
		}
	}
	return -1
}
