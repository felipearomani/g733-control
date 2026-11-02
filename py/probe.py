# /// script
# requires-python = ">=3.10"
# dependencies = ["hidapi"]
# ///
"""List every HID interface exposed by the Logitech G733 LIGHTSPEED dongle."""
import hid

VID, PID = 0x046D, 0x0AB5

for d in hid.enumerate(VID, PID):
    print(
        f"usage_page=0x{d.get('usage_page', 0):04x} usage=0x{d.get('usage', 0):04x} "
        f"iface={d.get('interface_number')} product={d.get('product_string')!r} "
        f"path={d.get('path')!r}"
    )
