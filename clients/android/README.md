# agents deck for Android

The deck on a phone or tablet: the same board as the desk panel and the menu bar app
(`clients/flutter/agents_ui`). A page of 16 slots is laid out 3×6, 4×4 or 6×3, whichever is closest to
the screen's shape, and scaled to it: a phone upright gets the tall board, a tablet on its side the
wide one, a 4:3 tablet the panel's square. It connects to `agentctl` on your
computer over TLS with its own certificate, exactly like the desk panel.

Pair it once: on the computer, `agentctl pair phone --qr --host <address>` (the computer's LAN
address, or its VPN address so the phone reaches the deck from anywhere), then **scan QR code** on
the app's first screen: a viewfinder opens, reads the code, and the phone makes its own key and
trades the code for its certificate ([docs/phone.md](../../docs/phone.md) has the details and what
it protects). Without a camera, **paste the code** takes what `agentctl pair phone` prints from the
clipboard, and **import phone.json** takes a bundle from `agentctl pair phone --bundle phone.json
--host <the computer's LAN address>`. `agentctl devices revoke phone` cuts the phone off.

A long press on the board opens the settings: which deck the phone is paired with and at what address,
whether the screen stays on while the deck is in front (on by default, like the panel), whether the
phone buzzes and posts a notification when an agent turns to waiting on you (on by default; see
[docs/phone.md](../../docs/phone.md#when-an-agent-needs-you)), pairing again (a QR code, a pasted
code or a bundle), forgetting the pairing, and the app's own version with **check for updates**. The app updates itself from this repository's GitHub releases: once per start it
compares the newest release's APK (by its `SHA256SUMS` entry) with the one it runs from, a strip on
the board says when a newer one is out, and **update to v…** downloads it, checks it and opens the
system installer ([docs/phone.md](../../docs/phone.md#updating-the-app)). The system's back gesture
closes an open card or the settings before it leaves the app.

The launcher icon is the deck's own critter in its propeller hat, black on the deck's orange;
`flutter test tool/render_icons_test.dart` renders its PNGs from the sprite the board draws.

Build: `flutter build apk --release` here (Flutter 3.35+, Android SDK 36), then install
`build/app/outputs/flutter-apk/app-release.apk` on the phone. With `android/key.properties` in
place the APK is signed with the release key ([docs/releasing.md](../../docs/releasing.md)),
otherwise with the debug key; the two cannot update each other. No Flutter plugins: the native side is
two Kotlin files over one method channel. `MainActivity.kt` has the app's folder, the system file
picker, the QR scanner (ZXing's viewfinder activity from `zxing-android-embedded`, the one library;
it asks for the camera permission when you tap scan), the screen's wake lock, the app's version and
APK checksum, and the hand-off of a downloaded update to the package installer; `Pairing.kt` makes
the key and the certificate request and does the pinned TLS request with the system's TLS stack.
`lib/updates.dart` talks to GitHub (plain Dart `HttpClient`, `crypto` for SHA-256).
