package hidpp

import (
	"errors"
	"fmt"
	"sync"
	"time"

	hid "github.com/sstallion/go-hid"
)

var (
	initOnce sync.Once
	initErr  error
)

type hidTransport struct{ dev *hid.Device }

func (h hidTransport) Write(p []byte) (int, error) { return h.dev.Write(p) }
func (h hidTransport) Close() error                { return h.dev.Close() }

func (h hidTransport) ReadWithTimeout(p []byte, timeout time.Duration) (int, error) {
	n, err := h.dev.ReadWithTimeout(p, timeout)
	if errors.Is(err, hid.ErrTimeout) {
		return 0, nil
	}
	return n, err
}

// Open finds the HID interface of vid:pid exposing usagePage (the HID++ page,
// 0xFF43 on Logitech headsets) and starts a Device on it.
func Open(vid, pid, usagePage uint16, deviceIndex byte) (*Device, error) {
	initOnce.Do(func() {
		if initErr = hid.Init(); initErr == nil {
			platformInit()
		}
	})
	if initErr != nil {
		return nil, fmt.Errorf("hidapi init: %w", initErr)
	}
	var path string
	err := hid.Enumerate(vid, pid, func(info *hid.DeviceInfo) error {
		if info != nil && path == "" && info.UsagePage == usagePage && info.Path != "" {
			path = info.Path
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("enumerate: %w", err)
	}
	if path == "" {
		return nil, ErrNotFound
	}
	dev, err := hid.OpenPath(path)
	if err != nil {
		return nil, fmt.Errorf("open %04x:%04x: %w", vid, pid, err)
	}
	return NewDevice(hidTransport{dev}, deviceIndex, DefaultTimeout), nil
}
