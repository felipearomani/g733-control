package hidpp

import hid "github.com/sstallion/go-hid"

// platformInit runs after hid.Init. hidapi seizes devices exclusively on macOS
// by default, which would stop the headset's volume wheel/media keys (same
// HID device) from reaching macOS and lock out other g733 processes.
func platformInit() { hid.SetOpenExclusive(false) }
