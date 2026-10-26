# agents deck for Android

The deck on a phone or tablet: the same board as the desk panel and the menu bar app
(`clients/flutter/agents_ui`), scaled to the screen's width. It connects to `agentctl` on your
computer over TLS with its own certificate, exactly like the desk panel.

Pair it once: on the computer, `agentctl pair phone --bundle phone.json --host <the computer's LAN
address>`, get `phone.json` onto the phone, and import it on the app's first screen. The bundle (with
the phone's private key) stays in the app's private folder; delete it on the computer. `agentctl
devices revoke phone` cuts the phone off.

Build: `flutter build apk --release` here (Flutter 3.35+, Android SDK 36), then install
`build/app/outputs/flutter-apk/app-release.apk` on the phone. The native side is one Kotlin file
(`MainActivity.kt`): the app's folder and the system file picker, over a method channel; no plugins.
