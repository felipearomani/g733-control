// Package g733 exposes the Logitech G733 headset features over HID++ 2.0:
// lighting (0x8070), equalizer (0x8310), sidetone (0x8300), battery and
// auto power-off (0x1F20), firmware (0x0003) and device name (0x0005).
package g733

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/felipearomani/g733-control/internal/hidpp"
	"github.com/felipearomani/g733-control/internal/i18n"
)

const (
	VendorID    = 0x046D
	UsagePage   = 0xFF43 // Logitech HID++ vendor page on the LIGHTSPEED dongle
	DeviceIndex = 0xFF   // headsets answer on the "direct" index
)

// ProductIDs are the known G733 dongle variants.
var ProductIDs = []uint16{0x0AB5, 0x0AFE, 0x0B1F}

const (
	FeatRoot       uint16 = 0x0000
	FeatFeatureSet uint16 = 0x0001
	FeatFirmware   uint16 = 0x0003
	FeatName       uint16 = 0x0005
	FeatGKeys      uint16 = 0x8010
	FeatLEDs       uint16 = 0x8070
	FeatSidetone   uint16 = 0x8300
	FeatEQ         uint16 = 0x8310
	FeatADC        uint16 = 0x1F20
)

// FeatureNames maps HID++ feature IDs to human names.
var FeatureNames = map[uint16]string{
	FeatRoot:       "Root",
	FeatFeatureSet: "FeatureSet",
	FeatFirmware:   "Firmware version",
	FeatName:       "Device name",
	FeatGKeys:      "G-keys",
	FeatLEDs:       "Color LED effects",
	FeatSidetone:   "Sidetone",
	FeatEQ:         "Equalizer",
	FeatADC:        "Battery voltage / power",
}

// Headset is an open connection to a G733.
type Headset struct {
	dev       *hidpp.Device
	ProductID uint16

	mu      sync.Mutex
	zones   []Zone
	ledCaps uint16
	eqInfo  *EQInfo
}

// Open finds the dongle and opens its HID++ interface.
func Open() (*Headset, error) {
	var lastErr error = hidpp.ErrNotFound
	for _, pid := range ProductIDs {
		d, err := hidpp.Open(VendorID, pid, UsagePage, DeviceIndex)
		if err == nil {
			return &Headset{dev: d, ProductID: pid}, nil
		}
		if !errors.Is(err, hidpp.ErrNotFound) {
			lastErr = err
		}
	}
	return nil, lastErr
}

func (h *Headset) Close() error                             { return h.dev.Close() }
func (h *Headset) Done() <-chan struct{}                    { return h.dev.Done() }
func (h *Headset) Notifications() <-chan hidpp.Notification { return h.dev.Notifications() }

// Ping reports whether the headset itself (not just the dongle) answers.
func (h *Headset) Ping() error { return h.dev.Ping() }

// FeatureIndex returns where a feature lives in this headset's table.
func (h *Headset) FeatureIndex(id uint16) (byte, error) {
	f, err := h.dev.Feature(id)
	return f.Index, err
}

func (h *Headset) call(id uint16, fn byte, params ...byte) ([]byte, error) {
	f, err := h.dev.Feature(id)
	if err != nil {
		return nil, err
	}
	return h.dev.Request(f.Index, fn, params...)
}

// Raw sends an arbitrary call to a feature (by ID) and returns the reply.
func (h *Headset) Raw(id uint16, fn byte, params ...byte) ([]byte, error) {
	return h.call(id, fn, params...)
}

// --- device information -----------------------------------------------------

// FeatureInfo is one row of the feature table.
type FeatureInfo struct {
	Index   byte
	ID      uint16
	Version byte
	Name    string
}

func (h *Headset) Features() ([]FeatureInfo, error) {
	fs, err := h.dev.ListFeatures()
	out := make([]FeatureInfo, 0, len(fs))
	for _, f := range fs {
		out = append(out, FeatureInfo{Index: f.Index, ID: f.ID, Version: f.Version, Name: FeatureNames[f.ID]})
	}
	return out, err
}

func (h *Headset) Name() (string, error) {
	r, err := h.call(FeatName, 0)
	if err != nil {
		return "", err
	}
	n := int(r[0])
	var b []byte
	for len(b) < n {
		chunk, err := h.call(FeatName, 1, byte(len(b)))
		if err != nil {
			return string(b), err
		}
		take := min(n-len(b), len(chunk))
		if take <= 0 {
			break
		}
		b = append(b, chunk[:take]...)
	}
	return strings.TrimRight(string(b), "\x00 "), nil
}

// Firmware is one firmware entity reported by 0x0003.
type Firmware struct {
	Kind    string
	Name    string
	Version string
}

func (f Firmware) String() string {
	return strings.TrimSpace(fmt.Sprintf("%s %s %s", f.Kind, f.Name, f.Version))
}

func (h *Headset) Firmware() ([]Firmware, error) {
	r, err := h.call(FeatFirmware, 0)
	if err != nil {
		return nil, err
	}
	var out []Firmware
	for i := 0; i < int(r[0]) && i < 8; i++ {
		e, err := h.call(FeatFirmware, 1, byte(i))
		if err != nil {
			return out, err
		}
		out = append(out, parseFirmware(e))
	}
	return out, nil
}

func parseFirmware(e []byte) Firmware {
	if len(e) < 8 {
		return Firmware{Kind: "?"}
	}
	switch e[0] & 0x0F {
	case 0, 1:
		kind := "Firmware"
		if e[0]&0x0F == 1 {
			kind = "Bootloader"
		}
		version := fmt.Sprintf("%02X.%02X", e[4], e[5])
		if build := uint16(e[6])<<8 | uint16(e[7]); build != 0 {
			version += fmt.Sprintf(".B%04X", build)
		}
		return Firmware{Kind: kind, Name: strings.TrimSpace(strings.Trim(string(e[1:4]), "\x00")), Version: version}
	case 2:
		return Firmware{Kind: "Hardware", Version: fmt.Sprintf("rev %d", e[1])}
	default:
		return Firmware{Kind: "Other"}
	}
}

// --- sidetone (0x8300) --------------------------------------------------------

// Sidetone returns the mic monitoring level, 0-100.
func (h *Headset) Sidetone() (int, error) {
	r, err := h.call(FeatSidetone, 0)
	if err != nil {
		return 0, err
	}
	return int(r[0]), nil
}

func (h *Headset) SetSidetone(level int) error {
	_, err := h.call(FeatSidetone, 1, byte(clamp(level, 0, 100)))
	return err
}

// --- auto power-off (0x1F20 v2+) ---------------------------------------------

// AutoOff returns the idle minutes before the headset powers off (0 = never).
func (h *Headset) AutoOff() (int, error) {
	f, err := h.dev.Feature(FeatADC)
	if err != nil {
		return 0, err
	}
	if f.Version < 2 {
		return 0, i18n.Errorf("%w: auto power-off needs 0x1F20 v2+", hidpp.ErrUnsupported)
	}
	r, err := h.dev.Request(f.Index, 1)
	if err != nil {
		return 0, err
	}
	return int(r[0]), nil
}

func (h *Headset) SetAutoOff(minutes int) error {
	f, err := h.dev.Feature(FeatADC)
	if err != nil {
		return err
	}
	if f.Version < 2 {
		return i18n.Errorf("%w: auto power-off needs 0x1F20 v2+", hidpp.ErrUnsupported)
	}
	_, err = h.dev.Request(f.Index, 2, byte(clamp(minutes, 0, 255)))
	return err
}

func clamp(v, lo, hi int) int {
	return max(lo, min(hi, v))
}
