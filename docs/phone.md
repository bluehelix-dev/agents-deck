# Phone app

A phone app built from the same Flutter package as the desk panel and the menu bar app, paired with
the deck like any other network device (see [protocol.md](protocol.md#devices-and-security)). The
Android app exists (`clients/android`, [its README](../clients/android/README.md)); iOS doesn't yet.

## Pairing with a QR code

1. On the computer running agentctl: `agentctl pair phone --qr`. The terminal shows a QR code
   holding three things: the listener's address, the deck's CA fingerprint, and a one-time pairing
   code (2 minutes, single use). The address is the computer's LAN address, not its name (a phone
   can't resolve a Windows machine name or a `.local` name); if the computer has several, or the
   phone reaches it some other way (a VPN), say which: `--host <address>`. The listener's
   certificate covers the computer's addresses, so an IP verifies fine.
2. In the app, tap **scan QR code**. A viewfinder opens in the app (it asks for the camera the
   first time); hold the phone so the code is in view, and the app reads it and closes the
   viewfinder by itself.
3. The app makes its own EC P-256 key, connects to the address, and trusts the server only if its
   certificate chains to a CA with exactly the scanned fingerprint (a look-alike server on the
   Wi-Fi is refused). It sends `POST /v1/pair {"code", "csr"}`; the deck checks the code, signs
   the request, and records the phone in `devices.json` with its scope. The app keeps the answer
   (its certificate, the CA, its key) in its private folder and connects at once. It stays paired
   until you forget the pairing in the app's settings (a long press on the board) or run
   `agentctl devices revoke phone` on the computer; do both to be rid of it.

The QR code is the out-of-band channel: whoever can see the computer's screen within those two
minutes can pair. The code's payload is JSON, the same three fields `agentctl pair phone` prints
without `--qr`, the fingerprint without its colons to keep the code small:

```json
{"url": "https://192.0.2.10:7341", "ca": "sha256:0123…CDEF", "code": "…"}
```

Scanning needs Android 10 or newer (the pairing request uses the system's TLS 1.3) and a camera.
Without a camera, copy what `agentctl pair phone` prints and tap **paste the code**: the same
pairing, from the clipboard. Without either, the bundle below still works.

The phone must reach the listener (`:7341`): the same network, or a VPN (Tailscale or WireGuard) to
it. Never open `:7341` to the internet. The board takes whichever of its shapes (3×6, 4×4, 6×3) is
closest to the screen's and fills it; cards get a back button since there's no edge swipe, and the
system's back gesture closes a card or the settings before it leaves the app.

### What this does and doesn't protect

The phone's key is made on the phone and never leaves it: nothing with the key in it travels in a
file or a message, and the computer never has it. That is the point of the design below. What the
app doesn't do yet is keep the key in the phone's secure hardware (StrongBox or the Keystore): the
deck client in the Flutter package does TLS with the key as PEM, so the key is a file in the app's
private folder, which no other app can read, like the desk panel's. A copied backup of the app's
data could impersonate the phone until `agentctl devices revoke phone`; moving TLS into native
code with a Keystore-held key would close that, and is the natural next step.

The server's address isn't checked against its certificate during pairing: the pinned CA is private
to one deck and signs only that deck's listener, so the CA fingerprint is the identity. Afterwards
the app connects the way the panel does: it verifies the server against the CA and, when the
address isn't one the certificate covers, accepts exactly the listener certificate it paired with.

## Pairing with a bundle

The other way, the desk panel's, with a bundle you carry over yourself:

1. On the computer: `agentctl pair phone --bundle phone.json --host <its LAN address>`. The bundle
   holds the listener's URL, the deck's CA, and a certificate and key made for the phone.
2. Get `phone.json` onto the phone (a cable, a drive, a message to yourself), then delete it on the
   computer: it holds the phone's private key.
3. In the app, tap **import phone.json** (on the first screen, or in the settings) and pick the
   file. The app checks it and keeps it in its private folder.

The difference from the QR code: the key is made on the computer and travels in a file. For your own
phone on your own network that's the desk panel's trade-off, and revoking is the same one command.

## The design: pairing with a QR code

The phone has to trust the deck, and the deck has to trust the phone, before either has talked to
the other. Baking a certificate into the app doesn't solve it: every install of the app would carry the
same certificate and key, anyone can pull them out of the app package, and so they would prove only
that someone has the app, not that it's *your* phone. A shared secret in a published app is not a
secret.

What the app can carry is *code*, not identity. The identity is made on the phone, and the trust is
carried by something you can see: a QR code on the computer's screen.

1. On the computer: `agentctl pair phone --qr` shows a QR code with three things: the listener's
   address, the deck's CA fingerprint, and a one-time pairing code (2 minutes, single use).
2. The app scans it and generates its own key pair in the phone's secure hardware (Secure Enclave on
   iOS, StrongBox/Keystore on Android), marked non-exportable.
3. It connects to the address, checks that the server's certificate chains to a CA with exactly the
   scanned fingerprint (so a look-alike server on the Wi-Fi is refused), and sends
   `POST /v1/pair {"code", "csr"}`.
4. The deck checks the code, signs the request and records the phone in `devices.json` with its scope.
   From then on the phone connects with that certificate, like the panel.

The QR code is the out-of-band channel: whoever can see the computer's screen within those two
minutes can pair. The phone's private key never leaves its secure hardware, so a stolen backup or a
copied app can't impersonate it, and `agentctl devices revoke phone` cuts it off.

Of this, the Android app does everything except the secure hardware (see above): the key is born on
the phone, but lives in a file.

## What signing the app adds

Code-signing (App Store, TestFlight, Play) proves the app wasn't tampered with. It can be checked by
the deck too (Apple App Attest, Play Integrity), which would let the deck refuse look-alike apps. That
is a later, optional layer on top of pairing, not a replacement for it.
