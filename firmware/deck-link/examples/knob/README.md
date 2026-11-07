# knob

A rotary encoder with a push button on an ESP32-S3.

- **Turn:** pick an agent.
- **Press:** open its prompt (on an agent without a prompt: focus its terminal).
- In a prompt, **turn** to pick an option (`*` marks the primary one) and **press** to answer;
  a **long press** goes back. If the agent asked something new while you turned, the press sends
  nothing: it says "prompt changed" and starts the pick over on the new prompt.

Deck Link uses the S3's native USB port. What the knob points at is printed as text on the UART port
(`Serial0`, the DevKitC-1's "UART" USB connector), so the Deck Link port carries frames only. Build the
`-oled` environment to show it on an SSD1306 as well.

It subscribes to `FIELDS_TILE | FIELDS_PROMPT`. Answering and focusing need scope **act**:

```sh
agentctl devices approve knob-<id>
agentctl devices scope knob-<id> act
```

## Parts and wiring

- ESP32-S3-DevKitC-1
- a quadrature rotary encoder with push button (e.g. EC11 / KY-040)
- optional: SSD1306 128×64 OLED, I²C, address 0x3C

| encoder | ESP32-S3 |
|---|---|
| A (CLK) | GPIO4 |
| B (DT) | GPIO5 |
| switch (SW) | GPIO6 |
| C / GND, other switch pin | GND |

Internal pull-ups are used; a KY-040 module's own pull-ups are fine too. Override pins with
`-DENC_A=… -DENC_B=… -DENC_BUTTON=…`, and `-DENC_STEPS=2` for encoders with two steps per detent.
OLED: SDA GPIO8, SCL GPIO9, VCC 3V3, GND.

## Build and flash

```sh
pio run -d firmware/deck-link/examples/knob                      # text view on Serial0
pio run -d firmware/deck-link/examples/knob -e esp32-s3-devkitc-1-oled
pio run -d firmware/deck-link/examples/knob -t upload
agentctl link add <the native USB port>
```
