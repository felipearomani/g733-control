# /// script
# requires-python = ">=3.10"
# dependencies = ["hidapi"]
# ///
"""Control a Logitech G733 headset directly over HID++ 2.0 (no G HUB).

Usage:
    uv run g733.py features              # list HID++ features the headset exposes
    uv run g733.py lights                # show LED zones and supported effects
    uv run g733.py lights off
    uv run g733.py lights static [RRGGBB]
    uv run g733.py lights breathe [RRGGBB] [--period MS]
    uv run g733.py lights cycle [--period MS]
"""
from __future__ import annotations

import argparse
import struct
import sys
import time

import hid

VID, PID = 0x046D, 0x0AB5          # G733 LIGHTSPEED dongle
HIDPP_USAGE_PAGE = 0xFF43          # Logitech vendor page carrying HID++ long reports
REPORT_LONG = 0x11                 # 20-byte HID++ report
REPORT_LEN = 20
DEVICE_INDEX = 0xFF                # headsets answer on the "direct" index
SW_ID = 0x0E                       # any non-zero value; tells our replies from notifications
TIMEOUT_S = 1.5

FEATURE_ROOT = 0x0000
FEATURE_SET = 0x0001
FEATURE_COLOR_LED_EFFECTS = 0x8070

FEATURE_NAMES = {
    0x0000: "Root",
    0x0001: "FeatureSet",
    0x0003: "DeviceFwVersion",
    0x0005: "DeviceName",
    0x0007: "DeviceFriendlyName",
    0x1000: "BatteryStatus",
    0x1004: "UnifiedBattery",
    0x1F20: "ADCMeasurement (battery voltage)",
    0x8070: "ColorLEDEffects",
    0x8071: "RGBEffects",
    0x8300: "Sidetone",
    0x8310: "Equalizer",
}

# Effect IDs from the 0x8070 spec, with the byte offset of each parameter
# inside the 10-byte effect-parameter block.
EFFECT_DISABLED = 0x0000
EFFECT_STATIC = 0x0001
EFFECT_CYCLE = 0x0003
EFFECT_BREATHE = 0x000A
EFFECT_NAMES = {
    0x0000: "off",
    0x0001: "static",
    0x0002: "pulse",
    0x0003: "cycle",
    0x0008: "boot",
    0x0009: "demo",
    0x000A: "breathe",
    0x000B: "ripple",
}

HIDPP_ERRORS = {
    0x01: "Unknown",
    0x02: "InvalidArgument",
    0x03: "OutOfRange",
    0x04: "HWError",
    0x05: "LogitechInternal",
    0x06: "InvalidFeatureIndex",
    0x07: "InvalidFunctionID",
    0x08: "Busy",
    0x09: "Unsupported",
}


class HidppError(RuntimeError):
    pass


class G733:
    def __init__(self) -> None:
        path = self._find_path()
        self.dev = hid.device()
        self.dev.open_path(path)
        self._feature_cache: dict[int, int] = {}

    @staticmethod
    def _find_path() -> bytes:
        for d in hid.enumerate(VID, PID) or []:
            if d.get("usage_page") == HIDPP_USAGE_PAGE and d.get("path"):
                return d["path"]
        raise SystemExit("G733 dongle not found (is the LIGHTSPEED receiver plugged in?)")

    def close(self) -> None:
        self.dev.close()

    # --- HID++ transport -------------------------------------------------

    def request(self, feature_index: int, function: int, *params: int) -> bytes:
        """Send one HID++ 2.0 request and return the reply's parameter bytes."""
        func_sw = ((function & 0x0F) << 4) | SW_ID
        msg = [REPORT_LONG, DEVICE_INDEX, feature_index, func_sw, *params]
        msg += [0] * (REPORT_LEN - len(msg))
        self.dev.write(msg[:REPORT_LEN])

        deadline = time.monotonic() + TIMEOUT_S
        while time.monotonic() < deadline:
            data = self.dev.read(64, 200) or []
            if len(data) < 4 or data[0] != REPORT_LONG or data[1] != DEVICE_INDEX:
                continue  # other reports (media keys, short HID++ etc.)
            if data[2] == 0xFF and len(data) >= 6 and data[3] == feature_index and data[4] == func_sw:
                code = data[5]
                raise HidppError(f"HID++ error 0x{code:02x} ({HIDPP_ERRORS.get(code, '?')})")
            if data[2] == feature_index and data[3] == func_sw:
                return bytes(data[4:])
            # anything else is an unsolicited notification (battery, mute...) -> ignore
        raise TimeoutError("no reply from headset (is it switched on and paired?)")

    def feature_index(self, feature_id: int) -> int | None:
        if feature_id in self._feature_cache:
            return self._feature_cache[feature_id]
        reply = self.request(0x00, 0, feature_id >> 8, feature_id & 0xFF)
        index = reply[0] if reply else 0
        result = index if index or feature_id == FEATURE_ROOT else None
        if result is not None:
            self._feature_cache[feature_id] = result
        return result

    # --- generic introspection ------------------------------------------

    def list_features(self) -> list[tuple[int, int, int]]:
        fs = self.feature_index(FEATURE_SET)
        if fs is None:
            return []
        count = self.request(fs, 0)[0]
        out = []
        for i in range(1, count + 1):
            r = self.request(fs, 1, i)
            if len(r) >= 4:
                out.append((i, (r[0] << 8) | r[1], r[3]))  # index, feature id, version
        return out

    # --- 0x8070 Color LED Effects ---------------------------------------

    def _led_index(self) -> int:
        idx = self.feature_index(FEATURE_COLOR_LED_EFFECTS)
        if idx is None:
            raise SystemExit("headset does not expose feature 0x8070 (Color LED Effects)")
        return idx

    def led_zones(self) -> list[dict]:
        """Return [{zone, location, effects: {effect_id: effect_index}}]."""
        idx = self._led_index()
        info = self.request(idx, 0)
        zone_count = info[0] if info else 0
        zones = []
        for z in range(zone_count):
            zi = self.request(idx, 1, z, 0xFF, 0x00)
            if len(zi) < 4:
                continue
            location = (zi[1] << 8) | zi[2]
            effect_count = zi[3]
            effects: dict[int, int] = {}
            for e in range(effect_count):
                ei = self.request(idx, 2, z, e, 0x00)
                if len(ei) >= 4:
                    effects[(ei[2] << 8) | ei[3]] = e
            zones.append({"zone": z, "location": location, "effects": effects})
        return zones

    def set_effect(self, effect_id: int, params: bytes = b"") -> int:
        """Apply an effect to every zone that supports it. Returns zones touched."""
        idx = self._led_index()
        block = (params + bytes(10))[:10]
        touched = 0
        for zone in self.led_zones():
            e_index = zone["effects"].get(effect_id)
            if e_index is None:
                print(f"  zone {zone['zone']}: effect {EFFECT_NAMES.get(effect_id, hex(effect_id))} unsupported, skipped")
                continue
            self.request(idx, 3, zone["zone"], e_index, *block)
            touched += 1
        return touched


def parse_color(text: str) -> bytes:
    text = text.lstrip("#")
    if len(text) != 6:
        raise argparse.ArgumentTypeError("color must be RRGGBB, e.g. ff0000")
    try:
        return bytes.fromhex(text)
    except ValueError as exc:
        raise argparse.ArgumentTypeError(f"invalid color {text!r}") from exc


def main() -> int:
    ap = argparse.ArgumentParser(description="Logitech G733 HID++ control")
    sub = ap.add_subparsers(dest="cmd", required=True)
    sub.add_parser("features", help="list HID++ features")
    lp = sub.add_parser("lights", help="LED control")
    lp.add_argument("mode", nargs="?", choices=["off", "static", "breathe", "cycle"])
    lp.add_argument("color", nargs="?", type=parse_color, default=bytes.fromhex("00b6ff"))
    lp.add_argument("--period", type=int, default=None, help="effect period in ms")
    lp.add_argument("--intensity", type=int, default=100, help="0-100")
    args = ap.parse_args()

    g = G733()
    try:
        if args.cmd == "features":
            for i, fid, ver in g.list_features():
                print(f"[{i:2d}] 0x{fid:04x} v{ver}  {FEATURE_NAMES.get(fid, '')}")
            return 0

        if args.mode is None:
            for z in g.led_zones():
                names = ", ".join(EFFECT_NAMES.get(fid, f"0x{fid:04x}") for fid in z["effects"])
                print(f"zone {z['zone']} (location 0x{z['location']:04x}): {names}")
            return 0

        intensity = max(0, min(100, args.intensity))
        if args.mode == "off":
            n = g.set_effect(EFFECT_DISABLED)
        elif args.mode == "static":
            n = g.set_effect(EFFECT_STATIC, args.color)
        elif args.mode == "breathe":
            period = args.period or 3000
            n = g.set_effect(EFFECT_BREATHE, args.color + struct.pack(">HBB", period, 0, intensity))
        else:  # cycle
            period = args.period or 10000
            n = g.set_effect(EFFECT_CYCLE, bytes(5) + struct.pack(">HB", period, intensity))
        print(f"lights {args.mode}: applied to {n} zone(s)")
        return 0 if n else 1
    except (HidppError, TimeoutError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1
    finally:
        g.close()


if __name__ == "__main__":
    raise SystemExit(main())
