// Package hats gives every agent a pixel hat picked by its working folder, so
// agents of one project look alike on the deck. A stable hash of the folder
// picks the shape and the colour; config.json "hats" overrides it per folder.
package hats

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// Shapes, in hash order (adding one reshuffles automatic hats, so append only).
var Shapes = []string{"hard hat", "cap", "beanie", "helmet", "wizard", "beret", "chef",
	"crown", "headphones", "propeller", "top hat", "cowboy", "bandana",
	"pirate", "viking", "sombrero", "fez", "party hat", "mortarboard", "bowler", "nightcap", "halo"}

// Colors: never the status orange.
var Colors = []string{"teal", "blue", "yellow", "green", "purple", "pink", "sky", "ink"}

// None is the shape for "no hat".
const None = "none"

// Auto picks the hat for a working folder: shape and colour from one stable
// hash (FNV-1a 32), so the same folder always wears the same hat.
func Auto(folder string) model.Hat {
	h := fnv.New32a()
	_, _ = h.Write([]byte(Clean(folder)))
	v := h.Sum32()
	return model.Hat{Shape: Shapes[v%uint32(len(Shapes))], Color: Colors[(v>>8)%uint32(len(Colors))], Auto: true}
}

// Clean normalizes a folder key (absolute, no trailing slash). On Windows
// the drive letter is a capital: Claude Code reports one folder as c:\… and
// C:\…, and both should wear one hat.
func Clean(folder string) string {
	if folder == "" {
		return ""
	}
	folder = filepath.Clean(folder)
	if runtime.GOOS == "windows" {
		folder = upperDrive(folder)
	}
	return folder
}

// upperDrive capitalizes a leading drive letter ("c:" to "C:").
func upperDrive(p string) string {
	if len(p) >= 2 && p[1] == ':' && 'a' <= p[0] && p[0] <= 'z' {
		return string(p[0]-'a'+'A') + p[1:]
	}
	return p
}

// Override is a manual choice for one folder.
type Override struct {
	Shape string `json:"shape"`
	Color string `json:"color,omitempty"`
}

// Validate checks a shape and colour; an empty colour keeps the automatic one.
func Validate(shape, color string) error {
	if shape != None && !contains(Shapes, shape) {
		return fmt.Errorf("unknown hat %q (one of: %s, none)", shape, strings.Join(Shapes, ", "))
	}
	if color != "" && !contains(Colors, color) {
		return fmt.Errorf("unknown colour %q (one of: %s)", color, strings.Join(Colors, ", "))
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Book holds the overrides from config.json, re-read when the file changes.
type Book struct {
	Path string // config.json

	mu    sync.Mutex
	mtime time.Time
	size  int64
	over  map[string]Override
}

// For is the hat an agent working in folder wears (nil: no hat).
func (b *Book) For(folder string) *model.Hat {
	if folder == "" {
		return nil
	}
	folder = Clean(folder)
	hat := Auto(folder)
	if o, ok := b.overrides()[folder]; ok {
		if o.Shape == None {
			return nil
		}
		hat.Shape, hat.Auto = o.Shape, false
		if o.Color != "" {
			hat.Color = o.Color
		}
	}
	return &hat
}

// Overrides returns a copy of every manual choice, by folder.
func (b *Book) Overrides() map[string]Override {
	out := map[string]Override{}
	for k, v := range b.overrides() {
		out[k] = v
	}
	return out
}

func (b *Book) overrides() map[string]Override {
	b.mu.Lock()
	defer b.mu.Unlock()
	st, err := os.Stat(b.Path)
	if err != nil {
		b.over, b.mtime, b.size = nil, time.Time{}, 0
		return nil
	}
	if b.over != nil && st.ModTime().Equal(b.mtime) && st.Size() == b.size {
		return b.over
	}
	var c struct {
		Hats map[string]Override `json:"hats"`
	}
	if raw, err := os.ReadFile(b.Path); err == nil {
		_ = json.Unmarshal(raw, &c)
	}
	b.over = map[string]Override{}
	for k, v := range c.Hats {
		if Validate(v.Shape, v.Color) == nil {
			b.over[Clean(k)] = v
		}
	}
	b.mtime, b.size = st.ModTime(), st.Size()
	return b.over
}

// Set stores a manual hat for folder (auto: removes it), keeping every other
// key of config.json.
func (b *Book) Set(folder, shape, color string, auto bool) error {
	folder = Clean(folder)
	if folder == "" {
		return errors.New("no folder")
	}
	if !auto {
		if err := Validate(shape, color); err != nil {
			return err
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	cfg := map[string]any{}
	if raw, err := os.ReadFile(b.Path); err == nil {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("config.json is not valid JSON: %v", err)
		}
	}
	all := map[string]Override{}
	if raw, err := json.Marshal(cfg["hats"]); err == nil {
		_ = json.Unmarshal(raw, &all)
	}
	// Keys as Clean makes them, so going back to auto drops the override
	// whatever spelling it was stored under.
	clean := map[string]Override{}
	for k, v := range all {
		clean[Clean(k)] = v
	}
	all = clean
	if auto {
		delete(all, folder)
	} else {
		all[folder] = Override{Shape: shape, Color: color}
	}
	if len(all) == 0 {
		delete(cfg, "hats")
	} else {
		cfg["hats"] = all
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(b.Path), 0o700); err != nil {
		return err
	}
	tmp := b.Path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return err
	}
	b.over = nil // re-read on the next lookup
	return os.Rename(tmp, b.Path)
}

// Folders lists the overridden folders, sorted.
func (b *Book) Folders() []string {
	var out []string
	for k := range b.overrides() {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
