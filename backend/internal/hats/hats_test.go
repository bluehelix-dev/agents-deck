package hats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAutoIsStableAndVaried(t *testing.T) {
	a, b := Auto("/Users/sam/dev/checkout-api"), Auto("/Users/sam/dev/checkout-api/")
	if a != b || !a.Auto {
		t.Fatalf("same folder, same hat: %+v %+v", a, b)
	}
	shapes, colors := map[string]bool{}, map[string]bool{}
	for _, f := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p"} {
		h := Auto("/dev/" + f)
		shapes[h.Shape], colors[h.Color] = true, true
	}
	if len(shapes) < 6 || len(colors) < 4 {
		t.Fatalf("hash should vary both: %d shapes, %d colours", len(shapes), len(colors))
	}
	for _, c := range Colors {
		if c == "orange" {
			t.Fatal("orange is the status colour")
		}
	}
}

func TestOverridesInConfig(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfg, []byte(`{"term":"tmux"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b := &Book{Path: cfg}
	dir := "/Users/sam/dev/app"
	if h := b.For(dir); h == nil || !h.Auto {
		t.Fatalf("automatic first: %+v", h)
	}
	if err := b.Set(dir, "cowboy", "teal", false); err != nil {
		t.Fatal(err)
	}
	if h := b.For(dir); h.Shape != "cowboy" || h.Color != "teal" || h.Auto {
		t.Fatalf("override: %+v", h)
	}
	if err := b.Set(dir, "beret", "", false); err != nil { // keep the automatic colour
		t.Fatal(err)
	}
	if h := b.For(dir); h.Shape != "beret" || h.Color != Auto(dir).Color {
		t.Fatalf("shape only: %+v", h)
	}
	if err := b.Set(dir, None, "", false); err != nil {
		t.Fatal(err)
	}
	if h := b.For(dir); h != nil {
		t.Fatalf("none: %+v", h)
	}
	var c map[string]any
	raw, _ := os.ReadFile(cfg)
	_ = json.Unmarshal(raw, &c)
	if c["term"] != "tmux" || c["hats"] == nil {
		t.Fatalf("other keys kept: %s", raw)
	}
	if err := b.Set(dir, "", "", true); err != nil {
		t.Fatal(err)
	}
	if h := b.For(dir); !h.Auto {
		t.Fatalf("back to auto: %+v", h)
	}
	raw, _ = os.ReadFile(cfg)
	c = nil
	if json.Unmarshal(raw, &c); c["hats"] != nil {
		t.Fatalf("no empty hats key: %s", raw)
	}
	if b.Set(dir, "fedora", "", false) == nil || b.Set(dir, "cap", "orange", false) == nil {
		t.Fatal("unknown shapes and colours are refused")
	}
}

// docs/protocol.md gives the rule for the automatic hat so a client can
// draw the same one: it names the shapes in order and the modulus.
func TestProtocolDocsAutoHatRule(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "protocol.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.Join(strings.Fields(strings.ReplaceAll(string(b), "//", " ")), " ")
	if want := fmt.Sprintf("shape = h mod %d", len(Shapes)); !strings.Contains(doc, want) {
		t.Fatalf("docs/protocol.md doesn't say %q", want)
	}
	if want := "shape: " + strings.Join(Shapes, ", "); !strings.Contains(doc, want) {
		t.Fatalf("docs/protocol.md doesn't list the shapes in order: %q", want)
	}
}

// Windows reports the same folder as c:\… and C:\…: one key, one hat.
func TestDriveLetterCase(t *testing.T) {
	for in, want := range map[string]string{`c:\w\app`: `C:\w\app`, `C:\w\app`: `C:\w\app`, `/w/app`: `/w/app`, `c`: `c`, ``: ``} {
		if got := upperDrive(in); got != want {
			t.Errorf("upperDrive(%q) = %q, want %q", in, got, want)
		}
	}
	if runtime.GOOS != "windows" {
		return
	}
	if Auto(`c:\w\app`) != Auto(`C:\w\app`) {
		t.Fatal("two automatic hats for one folder")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"hats":{"c:\\w\\app":{"shape":"crown"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b := &Book{Path: path}
	if h := b.For(`C:\w\app`); h == nil || h.Shape != "crown" {
		t.Fatalf("override under the other spelling: %+v", h)
	}
	if err := b.Set(`C:\w\app`, "", "", true); err != nil {
		t.Fatal(err)
	}
	if h := b.For(`c:\w\app`); h == nil || !h.Auto {
		t.Fatalf("back to auto left the other spelling's override: %+v", h)
	}
}
