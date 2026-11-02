package g733

// Battery is a reading of feature 0x1F20 (ADC measurement).
type Battery struct {
	MilliVolts int
	Percent    int  // estimated from the voltage curve
	Active     bool // headset on and connected
	Charging   bool
}

// Voltage → percent curve for the G633/G733 family (from HeadsetControl).
var batteryCurve = []struct{ mv, pct int }{
	{4100, 100}, {3950, 80}, {3850, 60}, {3750, 40},
	{3650, 20}, {3500, 10}, {3300, 5}, {3150, 0},
}

// EstimatePercent linearly interpolates the battery curve.
func EstimatePercent(mv int) int {
	if mv >= batteryCurve[0].mv {
		return 100
	}
	for i := 1; i < len(batteryCurve); i++ {
		hi, lo := batteryCurve[i-1], batteryCurve[i]
		if mv >= lo.mv {
			return lo.pct + (mv-lo.mv)*(hi.pct-lo.pct)/(hi.mv-lo.mv)
		}
	}
	return 0
}

// ParseBattery decodes a 0x1F20 getMeasurement reply or event payload.
func ParseBattery(p []byte) Battery {
	if len(p) < 3 {
		return Battery{}
	}
	mv := int(p[0])<<8 | int(p[1])
	return Battery{
		MilliVolts: mv,
		Percent:    EstimatePercent(mv),
		Active:     p[2]&0x01 != 0 && mv > 0,
		Charging:   p[2]&0x02 != 0,
	}
}

func (h *Headset) Battery() (Battery, error) {
	r, err := h.call(FeatADC, 0)
	if err != nil {
		return Battery{}, err
	}
	return ParseBattery(r), nil
}
