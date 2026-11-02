package g733

import "testing"

func TestEstimatePercent(t *testing.T) {
	cases := map[int]int{5000: 100, 4100: 100, 3750: 40, 3735: 37, 3650: 20, 3150: 0, 0: 0}
	for mv, want := range cases {
		if got := EstimatePercent(mv); got != want {
			t.Errorf("EstimatePercent(%d) = %d, want %d", mv, got, want)
		}
	}
}

func TestParseBattery(t *testing.T) {
	b := ParseBattery([]byte{0x0e, 0x97, 0x01})
	if b.MilliVolts != 3735 || !b.Active || b.Charging {
		t.Fatalf("got %+v", b)
	}
	if c := ParseBattery([]byte{0x10, 0x00, 0x03}); !c.Charging || !c.Active {
		t.Fatalf("charging not detected: %+v", c)
	}
	if z := ParseBattery(nil); z.Active {
		t.Fatalf("empty payload must be inactive")
	}
}

// Real replies captured from a G733 (firmware U1 37.00.B0131).
func TestParseFirmware(t *testing.T) {
	f := parseFirmware([]byte{0x00, 0x55, 0x31, 0x20, 0x37, 0x00, 0x01, 0x31, 0x01, 0x0a, 0xb5})
	if f.Kind != "Firmware" || f.Name != "U1" || f.Version != "37.00.B0131" {
		t.Fatalf("got %+v", f)
	}
}

func TestParseEQ(t *testing.T) {
	info := parseEQInfo([]byte{0x0a, 0x0c, 0x01, 0x00, 0x00})
	if info.Min != -12 || info.Max != 12 {
		t.Fatalf("range %+v", info)
	}
	first := parseEQFrequencies([]byte{0x00, 0x00, 0x20, 0x00, 0x40, 0x00, 0x7d, 0x00, 0xfa, 0x01, 0xf4, 0x03, 0xe8, 0x07, 0xd0, 0x00}, 10)
	second := parseEQFrequencies([]byte{0x07, 0x0f, 0xa0, 0x1f, 0x40, 0x3e, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 3)
	want := []int{32, 64, 125, 250, 500, 1000, 2000, 4000, 8000, 16000}
	got := append(first, second...)
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("band %d = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestEQPresets(t *testing.T) {
	for _, p := range EQPresets {
		if len(p.Curve) != 10 {
			t.Errorf("%s: %d points", p.Name, len(p.Curve))
		}
		for _, g := range p.Curve {
			if g < -12 || g > 12 {
				t.Errorf("%s: gain %d out of range", p.Name, g)
			}
		}
	}
	if MatchEQPreset(make([]int, 10)) != 0 {
		t.Fatal("flat not matched")
	}
	if p, ok := FindEQPreset("bass"); !ok || p.Name != "Bass Boost" {
		t.Fatal("prefix lookup failed")
	}
	if p, ok := FindEQPreset("passos"); !ok || p.Name != "FPS / Footsteps" {
		t.Fatal("prefix lookup failed")
	}
}

func TestLightRoundTrip(t *testing.T) {
	cases := []Light{
		{Effect: EffectOff, Brightness: 100, PeriodMS: 4000},
		{Effect: EffectStatic, Color: RGB{0xFF, 0x00, 0x80}, Brightness: 100, PeriodMS: 4000},
		{Effect: EffectBreathe, Color: RGB{0x00, 0xB6, 0xFF}, PeriodMS: 4000, Brightness: 100},
		{Effect: EffectCycle, PeriodMS: 12000, Brightness: 60},
	}
	for _, l := range cases {
		p := encodeLight(l)
		got := decodeLight(l.Effect, p[:])
		if got != l {
			t.Errorf("round trip %v: got %+v want %+v", l.Effect, got, l)
		}
	}
	// Matches the breathe packet HeadsetControl sends to the G733.
	p := encodeLight(Light{Effect: EffectBreathe, Color: RGB{0x00, 0xB6, 0xFF}, PeriodMS: 4000, Brightness: 100})
	want := [10]byte{0x00, 0xb6, 0xff, 0x0f, 0xa0, 0x00, 0x64}
	if p != want {
		t.Fatalf("breathe params % x, want % x", p, want)
	}
}

func TestStaticBrightnessScalesColor(t *testing.T) {
	p := encodeLight(Light{Effect: EffectStatic, Color: RGB{200, 100, 0}, Brightness: 50})
	if p[0] != 100 || p[1] != 50 || p[2] != 0 {
		t.Fatalf("got % x", p[:3])
	}
}

func TestParseColor(t *testing.T) {
	for in, want := range map[string]RGB{
		"ff0000": {255, 0, 0}, "#00B6FF": {0, 0xB6, 0xFF}, "f0a": {0xFF, 0x00, 0xAA},
		"vermelho": {255, 0, 0}, "Red": {255, 0, 0}, "logitech": {0, 0xB6, 0xFF},
	} {
		got, err := ParseColor(in)
		if err != nil || got != want {
			t.Errorf("ParseColor(%q) = %v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "12345", "zzzzzz", "#1234567"} {
		if _, err := ParseColor(bad); err == nil {
			t.Errorf("ParseColor(%q) accepted", bad)
		}
	}
}
