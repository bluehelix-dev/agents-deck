# Phone app

A phone app built from the same Flutter package as the desk panel and the menu bar app, paired with
the deck like any other network device (see [protocol.md](protocol.md#devices-and-security)). The
Android app exists (`clients/android`, [its README](../clients/android/README.md)); iOS doesn't yet.

## What exists today: pairing with a bundle

The Android app pairs the way the desk panel does, with a bundle you carry over yourself:

1. On the computer running agentctl: `agentctl pair phone --bundle phone.json --host <its LAN
   address>`. The bundle holds the listener's URL, the deck's CA, and a certificate and key made for
   the phone. `--host` matters: the bundle's URL otherwise names the computer, and a phone can't
   resolve a Windows machine name or a `.local` name; the listener's certificate covers the
   computer's addresses, so an IP verifies fine.
2. Get `phone.json` onto the phone (a cable, a drive, a message to yourself), then delete it on the
   computer: it holds the phone's private key.
3. In the app, tap **import phone.json** and pick the file. The app checks it and keeps it in its
   private folder (no other app can read it). It connects within a couple of seconds and stays paired
   until `agentctl devices revoke phone`.

The phone must reach the listener (`:7341`): the same network, or a VPN (Tailscale or WireGuard) to
it. Never open `:7341` to the internet. The app's screen is the panel's 720×720 board scaled to the
phone's width; cards get a back button since there's no edge swipe.

What this skips, compared to the design below: the key is made on the computer and travels in a
file, rather than being born in the phone's secure hardware. For your own phone on your own network
that's the desk panel's trade-off, and revoking is the same one command.

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

`agentctl pair` already implements the server side (`POST /v1/pair` with a CSR); the `--qr` flag, the
scanner and the hardware-backed key in the app are what's missing.

## What signing the app adds

Code-signing (App Store, TestFlight, Play) proves the app wasn't tampered with. It can be checked by
the deck too (Apple App Attest, Play Integrity), which would let the deck refuse look-alike apps. That
is a later, optional layer on top of pairing, not a replacement for it.
