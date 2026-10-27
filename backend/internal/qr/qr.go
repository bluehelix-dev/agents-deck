// Package qr makes QR codes (ISO/IEC 18004, byte mode) for the terminal:
// `agentctl pair <name> --qr` shows the phone a pairing code to scan.
// Standard library only, like the rest of the daemon.
package qr

import (
	"errors"
	"strings"
)

// Level is the error-correction level: how much of the code may be unreadable.
type Level int

const (
	L Level = iota // 7%
	M              // 15%
	Q              // 25%
	H              // 30%
)

// Code is an encoded QR symbol: Size×Size modules, dark or light.
type Code struct {
	Version int // 1..40
	Size    int // 21..177 modules a side
	Mask    int // the mask pattern applied (0..7)
	dark    []bool
}

// Dark reports whether the module at column x, row y is dark. Outside the
// symbol everything is light (the quiet zone).
func (c *Code) Dark(x, y int) bool {
	if x < 0 || y < 0 || x >= c.Size || y >= c.Size {
		return false
	}
	return c.dark[y*c.Size+x]
}

// Terminal renders the code with half-block characters (two module rows per
// text line) and the four-module quiet zone, light modules in the terminal's
// text colour. Scanners read it either way round, so a dark or a light theme
// both work. Every line ends with "\n".
func (c *Code) Terminal() string {
	const quiet = 4
	var b strings.Builder
	n := c.Size + 2*quiet
	for y := 0; y < n; y += 2 {
		for x := 0; x < n; x++ {
			top := c.Dark(x-quiet, y-quiet)
			bottom := c.Dark(x-quiet, y+1-quiet)
			switch {
			case top && bottom:
				b.WriteByte(' ')
			case top:
				b.WriteString("▄") // light below
			case bottom:
				b.WriteString("▀") // light above
			default:
				b.WriteString("█")
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// Encode makes the smallest code that holds data (as bytes, no character
// set announced: scanners assume UTF-8 or Latin-1, which agree on ASCII).
func Encode(data []byte, lvl Level) (*Code, error) {
	if lvl < L || lvl > H {
		return nil, errors.New("qr: bad level")
	}
	version := 0
	for v := 1; v <= 40; v++ {
		if 4+countBits(v)+8*len(data) <= 8*dataCodewords(v, lvl) {
			version = v
			break
		}
	}
	if version == 0 {
		return nil, errors.New("qr: too much data for one code")
	}
	// Data bits: mode (byte), count, the bytes, terminator, padding.
	var bits bitWriter
	bits.add(4, 4)
	bits.add(len(data), countBits(version))
	for _, d := range data {
		bits.add(int(d), 8)
	}
	capacity := 8 * dataCodewords(version, lvl)
	bits.add(0, min(4, capacity-bits.n))
	bits.add(0, (8-bits.n%8)%8)
	for pad := 0xEC; bits.n < capacity; pad ^= 0xEC ^ 0x11 {
		bits.add(pad, 8)
	}
	code := &Code{Version: version, Size: 17 + 4*version}
	code.dark = make([]bool, code.Size*code.Size)
	fn := make([]bool, code.Size*code.Size) // function modules: fixed, never masked
	code.drawFunctionPatterns(fn)
	code.drawCodewords(fn, interleave(bits.bytes, version, lvl))
	// The mask with the lowest penalty; the format bits name it.
	scores := code.maskScores(fn, lvl)
	best := 0
	for m, s := range scores {
		if s < scores[best] {
			best = m
		}
	}
	code.applyMask(fn, best)
	code.drawFormatBits(fn, lvl, best)
	code.Mask = best
	return code, nil
}

// maskScores is the penalty of the symbol under each of the eight masks.
func (c *Code) maskScores(fn []bool, lvl Level) [8]int {
	var scores [8]int
	for m := range scores {
		c.applyMask(fn, m)
		c.drawFormatBits(fn, lvl, m)
		scores[m] = c.penalty()
		c.applyMask(fn, m) // undo (xor)
	}
	return scores
}

// countBits is the width of the byte-count field for a version.
func countBits(version int) int {
	if version <= 9 {
		return 8
	}
	return 16
}

type bitWriter struct {
	bytes []byte
	n     int
}

func (w *bitWriter) add(v, n int) {
	for i := n - 1; i >= 0; i-- {
		if w.n%8 == 0 {
			w.bytes = append(w.bytes, 0)
		}
		if v>>uint(i)&1 == 1 {
			w.bytes[w.n/8] |= 0x80 >> uint(w.n%8)
		}
		w.n++
	}
}

// ---- error correction --------------------------------------------------------

// ecBlocks describes how a version/level splits its codewords: ec codewords
// per block, then (count, data codewords) for each group of blocks.
type ecBlocks struct {
	ec     int
	groups [][2]int
}

func (e ecBlocks) data() int {
	n := 0
	for _, g := range e.groups {
		n += g[0] * g[1]
	}
	return n
}

func (e ecBlocks) blocks() int {
	n := 0
	for _, g := range e.groups {
		n += g[0]
	}
	return n
}

func dataCodewords(version int, lvl Level) int { return ecTable[version-1][lvl].data() }

// interleave splits the data into blocks, appends Reed-Solomon codewords to
// each, and interleaves them the way the symbol is read.
func interleave(data []byte, version int, lvl Level) []byte {
	e := ecTable[version-1][lvl]
	var blocks, ecs [][]byte
	pos := 0
	for _, g := range e.groups {
		for i := 0; i < g[0]; i++ {
			blk := data[pos : pos+g[1]]
			pos += g[1]
			blocks = append(blocks, blk)
			ecs = append(ecs, rsEncode(blk, e.ec))
		}
	}
	out := make([]byte, 0, len(data)+e.ec*e.blocks())
	longest := 0
	for _, b := range blocks {
		longest = max(longest, len(b))
	}
	for i := 0; i < longest; i++ {
		for _, b := range blocks {
			if i < len(b) {
				out = append(out, b[i])
			}
		}
	}
	for i := 0; i < e.ec; i++ {
		for _, b := range ecs {
			out = append(out, b[i])
		}
	}
	return out
}

// GF(256) with the QR polynomial x^8 + x^4 + x^3 + x^2 + 1.
func gfMul(a, b byte) byte {
	var p byte
	for b != 0 {
		if b&1 != 0 {
			p ^= a
		}
		carry := a & 0x80
		a <<= 1
		if carry != 0 {
			a ^= 0x1D
		}
		b >>= 1
	}
	return p
}

// rsEncode returns n Reed-Solomon codewords for data.
func rsEncode(data []byte, n int) []byte {
	// Generator: product of (x - 2^i), i = 0..n-1.
	gen := make([]byte, n)
	gen[n-1] = 1
	root := byte(1)
	for i := 0; i < n; i++ {
		for j := range gen {
			gen[j] = gfMul(gen[j], root)
			if j+1 < n {
				gen[j] ^= gen[j+1]
			}
		}
		root = gfMul(root, 2)
	}
	rem := make([]byte, n)
	for _, d := range data {
		factor := d ^ rem[0]
		copy(rem, rem[1:])
		rem[n-1] = 0
		for j := range gen {
			rem[j] ^= gfMul(gen[j], factor)
		}
	}
	return rem
}

// ---- the symbol ---------------------------------------------------------------

func (c *Code) set(fn []bool, x, y int, dark bool) {
	c.dark[y*c.Size+x] = dark
	fn[y*c.Size+x] = true
}

func (c *Code) drawFunctionPatterns(fn []bool) {
	// Timing patterns.
	for i := 0; i < c.Size; i++ {
		c.set(fn, 6, i, i%2 == 0)
		c.set(fn, i, 6, i%2 == 0)
	}
	// Finder patterns in three corners, with their separators.
	c.drawFinder(fn, 3, 3)
	c.drawFinder(fn, c.Size-4, 3)
	c.drawFinder(fn, 3, c.Size-4)
	// Alignment patterns, except where they'd overlap a finder.
	pos := alignmentPositions(c.Version)
	for i, ax := range pos {
		for j, ay := range pos {
			if (i == 0 && j == 0) || (i == 0 && j == len(pos)-1) || (i == len(pos)-1 && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					c.set(fn, ax+dx, ay+dy, max(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}
	c.drawFormatBits(fn, L, 0) // reserves the modules; redrawn once the mask is chosen
	c.drawVersion(fn)
}

func (c *Code) drawFinder(fn []bool, cx, cy int) {
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			x, y := cx+dx, cy+dy
			if x < 0 || y < 0 || x >= c.Size || y >= c.Size {
				continue
			}
			d := max(abs(dx), abs(dy))
			c.set(fn, x, y, d != 2 && d != 4)
		}
	}
}

// alignmentPositions are the centres of the alignment patterns, along each
// axis: evenly spaced from 6 to size-7 (version 32 is the one irregular row).
func alignmentPositions(version int) []int {
	if version == 1 {
		return nil
	}
	n := version/7 + 2
	step := (version*4 + n*2 + 1) / (n*2 - 2) * 2
	if version == 32 {
		step = 26
	}
	pos := make([]int, n)
	pos[0] = 6
	for i, p := n-1, 17+4*version-7; i >= 1; i, p = i-1, p-step {
		pos[i] = p
	}
	return pos
}

// formatBits are the 15 bits naming the level and mask: 5 data bits, 10
// BCH check bits, and a fixed xor so no code has an all-light format.
func formatBits(lvl Level, mask int) int {
	levelBits := [...]int{L: 1, M: 0, Q: 3, H: 2}
	data := levelBits[lvl]<<3 | mask
	rem := data
	for i := 0; i < 10; i++ {
		rem = rem<<1 ^ (rem>>9)*0x537
	}
	return (data<<10 | rem) ^ 0x5412
}

// versionBits are the 18 bits naming the version (7 and up): 6 data bits
// and 12 BCH check bits.
func versionBits(version int) int {
	rem := version
	for i := 0; i < 12; i++ {
		rem = rem<<1 ^ (rem>>11)*0x1F25
	}
	return version<<12 | rem
}

// drawFormatBits writes the level and mask next to the finders.
func (c *Code) drawFormatBits(fn []bool, lvl Level, mask int) {
	bits := formatBits(lvl, mask)
	bit := func(i int) bool { return bits>>uint(i)&1 == 1 }
	// Around the top-left finder.
	for i := 0; i <= 5; i++ {
		c.set(fn, 8, i, bit(i))
	}
	c.set(fn, 8, 7, bit(6))
	c.set(fn, 8, 8, bit(7))
	c.set(fn, 7, 8, bit(8))
	for i := 9; i < 15; i++ {
		c.set(fn, 14-i, 8, bit(i))
	}
	// Below the top-right finder and right of the bottom-left one.
	for i := 0; i < 8; i++ {
		c.set(fn, c.Size-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		c.set(fn, 8, c.Size-15+i, bit(i))
	}
	c.set(fn, 8, c.Size-8, true) // always dark
}

// drawVersion writes the version, BCH-coded, for versions 7 and up.
func (c *Code) drawVersion(fn []bool) {
	if c.Version < 7 {
		return
	}
	bits := versionBits(c.Version)
	for i := 0; i < 18; i++ {
		b := bits>>uint(i)&1 == 1
		a, bb := c.Size-11+i%3, i/3
		c.set(fn, a, bb, b)
		c.set(fn, bb, a, b)
	}
}

// drawCodewords places the bits in the zigzag of two-module columns, from the
// right, skipping function modules. Remainder modules stay light.
func (c *Code) drawCodewords(fn []bool, data []byte) {
	i := 0
	for right := c.Size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5 // the vertical timing pattern
		}
		for vert := 0; vert < c.Size; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				y := vert
				if (right+1)&2 == 0 {
					y = c.Size - 1 - vert // upward
				}
				if !fn[y*c.Size+x] && i < 8*len(data) {
					c.dark[y*c.Size+x] = data[i/8]>>uint(7-i%8)&1 == 1
					i++
				}
			}
		}
	}
}

// applyMask xors the data modules with mask pattern m (so applying it twice
// undoes it).
func (c *Code) applyMask(fn []bool, m int) {
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if fn[y*c.Size+x] {
				continue
			}
			var inv bool
			switch m {
			case 0:
				inv = (x+y)%2 == 0
			case 1:
				inv = y%2 == 0
			case 2:
				inv = x%3 == 0
			case 3:
				inv = (x+y)%3 == 0
			case 4:
				inv = (x/3+y/2)%2 == 0
			case 5:
				inv = x*y%2+x*y%3 == 0
			case 6:
				inv = (x*y%2+x*y%3)%2 == 0
			case 7:
				inv = ((x+y)%2+x*y%3)%2 == 0
			}
			if inv {
				c.dark[y*c.Size+x] = !c.dark[y*c.Size+x]
			}
		}
	}
}

// penalty scores a masked symbol as the standard does: long runs, 2×2 blocks,
// finder look-alikes and an uneven dark/light balance all cost.
func (c *Code) penalty() int {
	score := 0
	line := make([]bool, c.Size)
	for axis := 0; axis < 2; axis++ {
		for i := 0; i < c.Size; i++ {
			for j := 0; j < c.Size; j++ {
				if axis == 0 {
					line[j] = c.Dark(j, i)
				} else {
					line[j] = c.Dark(i, j)
				}
			}
			score += linePenalty(line)
		}
	}
	for y := 0; y+1 < c.Size; y++ {
		for x := 0; x+1 < c.Size; x++ {
			d := c.Dark(x, y)
			if d == c.Dark(x+1, y) && d == c.Dark(x, y+1) && d == c.Dark(x+1, y+1) {
				score += 3
			}
		}
	}
	dark := 0
	for _, d := range c.dark {
		if d {
			dark++
		}
	}
	// Ten points for every five percent the dark share is off half.
	total := c.Size * c.Size
	return score + 10*(abs(dark*2-total)*10/total)
}

// linePenalty: runs of five or more cost 3 plus one per extra module; a
// 1:1:3:1:1 finder look-alike with four light modules on a side costs 40.
func linePenalty(line []bool) int {
	score := 0
	for i := 0; i < len(line); {
		j := i
		for j < len(line) && line[j] == line[i] {
			j++
		}
		if run := j - i; run >= 5 {
			score += run - 2
		}
		i = j
	}
	// Beyond the symbol's edge is the quiet zone, which is light; a decoder's
	// finder-pattern search sees it that way (so does ZXing's mask scoring).
	finder := []bool{true, false, true, true, true, false, true}
	light := func(from, to int) bool {
		for _, d := range line[max(from, 0):min(to, len(line))] {
			if d {
				return false
			}
		}
		return true
	}
	for i := 0; i+len(finder) <= len(line); i++ {
		match := true
		for k, d := range finder {
			if line[i+k] != d {
				match = false
				break
			}
		}
		if match && (light(i-4, i) || light(i+len(finder), i+len(finder)+4)) {
			score += 40
		}
	}
	return score
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
