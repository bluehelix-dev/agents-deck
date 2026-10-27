package qr

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Every row of the error-correction table must account for exactly the
// codewords the symbol has room for (a typo in the table would not).
func TestTableMatchesLayout(t *testing.T) {
	for v := 1; v <= 40; v++ {
		c := &Code{Version: v, Size: 17 + 4*v}
		c.dark = make([]bool, c.Size*c.Size)
		fn := make([]bool, c.Size*c.Size)
		c.drawFunctionPatterns(fn)
		free := 0
		for _, f := range fn {
			if !f {
				free++
			}
		}
		for lvl := L; lvl <= H; lvl++ {
			e := ecTable[v-1][lvl]
			if got, want := e.data()+e.ec*e.blocks(), free/8; got != want {
				t.Errorf("version %d level %d: table has %d codewords, the symbol holds %d", v, lvl, got, want)
			}
			// Group 2 blocks hold one codeword more than group 1 (so the
			// interleaving order holds).
			if len(e.groups) == 2 && e.groups[1][1] != e.groups[0][1]+1 {
				t.Errorf("version %d level %d: block sizes %v", v, lvl, e.groups)
			}
		}
	}
}

func TestAlignmentPositions(t *testing.T) {
	for v, want := range map[int][]int{
		1: nil, 2: {6, 18}, 7: {6, 22, 38}, 14: {6, 26, 46, 66}, 21: {6, 28, 50, 72, 94},
		32: {6, 34, 60, 86, 112, 138}, 36: {6, 24, 50, 76, 102, 128, 154}, 40: {6, 30, 58, 86, 114, 142, 170},
	} {
		if got := alignmentPositions(v); !slices.Equal(got, want) {
			t.Errorf("version %d: %v, want %v", v, got, want)
		}
	}
}

// The standard's worked example: "HELLO WORLD" at 1-M, with its padding.
func TestReedSolomon(t *testing.T) {
	data := []byte{32, 91, 11, 120, 209, 114, 220, 77, 67, 64, 236, 17, 236, 17, 236, 17}
	want := []byte{196, 35, 39, 119, 235, 215, 231, 226, 93, 23}
	if got := rsEncode(data, 10); !bytes.Equal(got, want) {
		t.Errorf("ec codewords %v, want %v", got, want)
	}
}

func TestFormatAndVersionBits(t *testing.T) {
	if got := formatBits(M, 0); got != 0x5412 {
		t.Errorf("format M/0: %#x", got)
	}
	if got := formatBits(L, 0); got != 0x77C4 {
		t.Errorf("format L/0: %#x", got)
	}
	if got := formatBits(H, 7); got != 0x083B {
		t.Errorf("format H/7: %#x", got)
	}
	if got := versionBits(7); got != 0x07C94 {
		t.Errorf("version 7: %#x", got)
	}
	if got := versionBits(40); got != 0x28C69 {
		t.Errorf("version 40: %#x", got)
	}
}

func TestEncodePicksTheSmallestVersion(t *testing.T) {
	for _, tc := range []struct {
		n       int
		lvl     Level
		version int
	}{
		{0, L, 1}, {17, L, 1}, {18, L, 2}, {14, M, 1}, {15, M, 2}, {7, H, 1}, {8, H, 2},
		{192, L, 8}, {193, L, 9}, {230, L, 9}, {231, L, 10}, {2953, L, 40},
	} {
		c, err := Encode(bytes.Repeat([]byte{'x'}, tc.n), tc.lvl)
		if err != nil {
			t.Fatalf("%d bytes at %d: %v", tc.n, tc.lvl, err)
		}
		if c.Version != tc.version || c.Size != 17+4*tc.version {
			t.Errorf("%d bytes at level %d: version %d size %d, want version %d", tc.n, tc.lvl, c.Version, c.Size, tc.version)
		}
	}
	if _, err := Encode(bytes.Repeat([]byte{'x'}, 2954), L); err == nil {
		t.Error("2954 bytes fit?")
	}
}

// The finder patterns, timing patterns and the dark module are where every
// reader expects them, whatever the data.
func TestFixedPatterns(t *testing.T) {
	c, err := Encode([]byte(`{"url":"https://192.0.2.10:7341","code":"abc"}`), M)
	if err != nil {
		t.Fatal(err)
	}
	finder := func(x0, y0 int) {
		for dy := 0; dy < 7; dy++ {
			for dx := 0; dx < 7; dx++ {
				ring := dx == 0 || dy == 0 || dx == 6 || dy == 6
				core := dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4
				if got := c.Dark(x0+dx, y0+dy); got != (ring || core) {
					t.Errorf("finder at %d,%d: module %d,%d dark=%v", x0, y0, dx, dy, got)
				}
			}
		}
	}
	finder(0, 0)
	finder(c.Size-7, 0)
	finder(0, c.Size-7)
	for i := 8; i < c.Size-8; i++ {
		if c.Dark(i, 6) != (i%2 == 0) || c.Dark(6, i) != (i%2 == 0) {
			t.Errorf("timing pattern broken at %d", i)
		}
	}
	if !c.Dark(8, c.Size-8) {
		t.Error("the dark module is light")
	}
	if c.Dark(-1, 0) || c.Dark(0, c.Size) {
		t.Error("the quiet zone is dark")
	}
}

func TestTerminal(t *testing.T) {
	c, err := Encode([]byte("hello"), L)
	if err != nil {
		t.Fatal(err)
	}
	out := c.Terminal()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != (21+8+1)/2 {
		t.Errorf("%d lines", len(lines))
	}
	for i, l := range lines {
		if n := len([]rune(l)); n != 21+8 {
			t.Errorf("line %d: %d columns", i, n)
		}
		if i < 2 || i >= len(lines)-2 {
			if strings.Trim(l, "█") != "" {
				t.Errorf("quiet zone line %d: %q", i, l)
			}
		} else if !strings.HasPrefix(l, "████") || !strings.HasSuffix(l, "████") {
			t.Errorf("line %d lacks the quiet zone: %q", i, l)
		}
	}
	// The top-left finder: its first text line shows the ring's top edge
	// (row 0 dark, row 1 dark at the sides).
	if !strings.HasPrefix(lines[2], "████ ▄▄▄▄▄ █") {
		t.Errorf("finder line: %q", lines[2])
	}
}

// QR_PNG_DIR=<dir> writes sample codes as PNGs, each with a .txt of what it
// says, for a real decoder to check (ZXing, which the Android app uses; see
// the commit that added this package). Not a test on its own.
func TestWritePNGs(t *testing.T) {
	dir := os.Getenv("QR_PNG_DIR")
	if dir == "" {
		t.Skip("set QR_PNG_DIR=<dir> to write samples")
	}
	write := func(name, text string, lvl Level, inverted bool) {
		c, err := Encode([]byte(text), lvl)
		if err != nil {
			t.Fatal(err)
		}
		scale, quiet := 6, 4 // QR_SCALE: pixels per module
		if v, _ := strconv.Atoi(os.Getenv("QR_SCALE")); v > 0 {
			scale = v
		}
		n := (c.Size + 2*quiet) * scale
		img := image.NewGray(image.Rect(0, 0, n, n))
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				dark := c.Dark(x/scale-quiet, y/scale-quiet)
				if inverted {
					dark = !dark
				}
				v := color.Gray{255}
				if dark {
					v = color.Gray{0}
				}
				img.SetGray(x, y, v)
			}
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: version %d mask %d", name, c.Version, c.Mask)
	}
	payload := `{"url":"https://192.0.2.10:7341","ca":"sha256:0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF","code":"abcdefghijklmnopqrst"}`
	write("payload", payload, M, false)
	write("payload-inverted", payload, M, true)
	// QR_PAYLOADS=<n>: that many pairing payloads with random fingerprints
	// and codes, what `agentctl pair --qr` really shows.
	if n, _ := strconv.Atoi(os.Getenv("QR_PAYLOADS")); n > 0 {
		rnd := rand.New(rand.NewSource(1))
		for i := 0; i < n; i++ {
			fp := make([]byte, 32)
			rnd.Read(fp)
			code := make([]byte, 15)
			rnd.Read(code)
			host := fmt.Sprintf("%d.%d.%d.%d", 10+rnd.Intn(183), rnd.Intn(256), rnd.Intn(256), 1+rnd.Intn(254))
			p := fmt.Sprintf(`{"ca":"sha256:%X","code":"%s","url":"https://%s:7341"}`, fp,
				strings.TrimRight(base64.URLEncoding.EncodeToString(code), "="), host)
			write(fmt.Sprintf("pairing%02d", i), p, M, i%2 == 1)
		}
	}
	// Every size class, every level: a long run of text at lengths that land
	// in versions 1, 3, 7, 10, 15, 27 and 40.
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. 0123456789 ", 60)
	lengths := []int{10, 50, 150, 240, 500, 1200, 2900}
	if l := os.Getenv("QR_LENGTHS"); l != "" {
		lengths = nil
		for _, f := range strings.Split(l, ",") {
			n, _ := strconv.Atoi(f)
			lengths = append(lengths, n)
		}
	}
	for _, n := range lengths {
		for lvl, name := range []string{"L", "M", "Q", "H"} {
			sample := text[:min(n, len(text))]
			if _, err := Encode([]byte(sample), Level(lvl)); err != nil {
				continue // too long for this level
			}
			write(fmt.Sprintf("n%d-%s", n, name), sample, Level(lvl), false)
		}
	}
}

// QR_DUMP=<dir> writes the error-correction table and, for QR_TEXT at QR_LEVEL
// (0..3), the module matrix, in a form another encoder's output can be
// diffed against (see the commit that added this package).
func TestDump(t *testing.T) {
	dir := os.Getenv("QR_DUMP")
	if dir == "" {
		t.Skip("set QR_DUMP=<dir>")
	}
	var tb strings.Builder
	for v := 1; v <= 40; v++ {
		for lvl := L; lvl <= H; lvl++ {
			e := ecTable[v-1][lvl]
			fmt.Fprintf(&tb, "v%d %d ec=%d", v, lvl, e.ec)
			for _, g := range e.groups {
				fmt.Fprintf(&tb, " %dx%d", g[0], g[1])
			}
			tb.WriteByte('\n')
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "table.txt"), []byte(tb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if text := os.Getenv("QR_TEXT"); text != "" {
		lvl := Level(os.Getenv("QR_LEVEL")[0] - '0')
		c, err := Encode([]byte(text), lvl)
		if err != nil {
			t.Fatal(err)
		}
		var mb strings.Builder
		fmt.Fprintf(&mb, "version %d mask %d\n", c.Version, c.Mask)
		for y := 0; y < c.Size; y++ {
			for x := 0; x < c.Size; x++ {
				if c.Dark(x, y) {
					mb.WriteByte('1')
				} else {
					mb.WriteByte('0')
				}
			}
			mb.WriteByte('\n')
		}
		if err := os.WriteFile(filepath.Join(dir, "matrix.txt"), []byte(mb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
