# /// script
# requires-python = ">=3.10"
# dependencies = ["hidapi"]
# ///
"""Read-only probe of every G733 HID++ feature (no setters are called)."""
from g733 import G733, HidppError


def hx(b: bytes) -> str:
    return b.hex(" ")


def call(g: G733, label: str, idx: int, fn: int, *params: int) -> bytes | None:
    try:
        r = g.request(idx, fn, *params)
        print(f"  {label:<34} fn{fn:x} {params!s:<16} -> {hx(r)}")
        return r
    except (HidppError, TimeoutError) as exc:
        print(f"  {label:<34} fn{fn:x} {params!s:<16} -> {exc}")
        return None


g = G733()
try:
    for fid in (0x0003, 0x0005, 0x8070, 0x8010, 0x8310, 0x8300, 0x1F20):
        r = g.request(0x00, 0, fid >> 8, fid & 0xFF)
        print(f"root getFeature 0x{fid:04x} -> {hx(r[:3])} (index, type, version)")

    fw = g.feature_index(0x0003)
    print("0x0003 FW version")
    c = call(g, "getEntityCount", fw, 0)
    for e in range((c or b"\0")[0]):
        call(g, f"getFwInfo({e})", fw, 1, e)

    dn = g.feature_index(0x0005)
    print("0x0005 DeviceName")
    call(g, "getCount", dn, 0)
    call(g, "getName(0)", dn, 1, 0)
    call(g, "getType", dn, 2)

    led = g.feature_index(0x8070)
    print("0x8070 ColorLEDEffects")
    call(g, "getInfo", led, 0)
    for z in (0, 1):
        call(g, f"getZoneInfo({z})", led, 1, z, 0xFF, 0)
        for e in range(4):
            call(g, f"getZoneEffectInfo({z},{e})", led, 2, z, e, 0)
        call(g, f"getZoneEffect({z}) fnE", led, 0xE, z)

    gk = g.feature_index(0x8010)
    print("0x8010 (G-keys?)")
    call(g, "fn0", gk, 0)

    eq = g.feature_index(0x8310)
    print("0x8310 Equalizer")
    info = call(g, "getInfo", eq, 0)
    call(g, "getFreqs(0)", eq, 1, 0)
    if info and info[0] > 7:
        call(g, "getFreqs(7)", eq, 1, 7)
    call(g, "getEQ(0)", eq, 2, 0)
    call(g, "getEQ(1)", eq, 2, 1)

    st = g.feature_index(0x8300)
    print("0x8300 Sidetone")
    call(g, "getSidetone", st, 0)

    adc = g.feature_index(0x1F20)
    print("0x1F20 ADC")
    call(g, "getMeasurement", adc, 0)
    call(g, "getPowerOff fn1", adc, 1)
finally:
    g.close()
