# agents deck for Android

The deck on a phone or tablet: the same board as the desk panel and the menu bar app
(`clients/flutter/agents_ui`). A page of 16 slots is laid out 3×6, 4×4 or 6×3, whichever is closest to
the screen's shape, and scaled to it: a phone upright gets the tall board, a tablet on its side the
wide one, a 4:3 tablet the panel's square. It connects to `agentctl` on your
computer over TLS with its own certificate, exactly like the desk panel.

Pair it once: on the computer, `agentctl pair phone --qr`, then **scan QR code** on the app's first
screen: the camera app opens, you photograph the code, and the phone makes its own key and trades
the code for its certificate ([docs/phone.md](../../docs/phone.md) has the details and what it
protects). Without a camera, **paste the code** takes what `agentctl pair phone` prints from the
clipboard, and **import phone.json** takes a bundle from `agentctl pair phone --bundle phone.json
--host <the computer's LAN address>`. `agentctl devices revoke phone` cuts the phone off.

A long press on the board opens the settings: which deck the phone is paired with and at what address,
whether the screen stays on while the deck is in front (on by default, like the panel), pairing again
(a QR code, a pasted code or a bundle), and forgetting the pairing. The system's back gesture closes an open card or the
settings before it leaves the app.

The launcher icon is the deck's own critter; `flutter test tool/render_icons_test.dart` renders its
PNGs from the sprite the board draws.

Build: `flutter build apk --release` here (Flutter 3.35+, Android SDK 36), then install
`build/app/outputs/flutter-apk/app-release.apk` on the phone. No Flutter plugins: the native side is
three Kotlin files over one method channel. `MainActivity.kt` has the app's folder, the system file
picker, the camera app (a photo, not a live viewfinder: no camera code and no camera permission) and
the screen's wake lock;
`QrPhoto.kt` reads the QR code in the photo with ZXing (`com.google.zxing:core`, pure Java, the one
library); `Pairing.kt` makes the key and the certificate request and does the pinned TLS request
with the system's TLS stack.
