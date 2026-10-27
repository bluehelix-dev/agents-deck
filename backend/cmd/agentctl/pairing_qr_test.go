package main

import (
	"encoding/json"
	"testing"
)

// The app (Pairing.kt) wants "sha256:" and 64 hex digits: the colons between
// the digits go, the one after the prefix stays.
func TestPairingQRKeepsThePrefix(t *testing.T) {
	fp := "sha256:AD:0D:C0:18:5C:E5:B9:6F:E4:35:9E:1D:A2:97:31:29:CA:89:A2:B3:80:A1:E6:C6:57:DC:4B:E7:0D:A2:D1:E5"
	var got map[string]string
	if err := json.Unmarshal(pairingQR("https://192.0.2.10:7341", fp, "c0de"), &got); err != nil {
		t.Fatal(err)
	}
	want := "sha256:AD0DC0185CE5B96FE4359E1DA2973129CA89A2B380A1E6C657DC4BE70DA2D1E5"
	if got["ca"] != want {
		t.Errorf("ca = %q, want %q", got["ca"], want)
	}
	if got["url"] != "https://192.0.2.10:7341" || got["code"] != "c0de" {
		t.Errorf("payload = %v", got)
	}
}
