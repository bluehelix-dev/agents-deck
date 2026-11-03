package link

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The largest frame the deck can send with link.options is nanopb's
// Frame_size plus the magic, version and CRC: the kit's header, its README
// and the protocol docs give that figure, so a device author who builds with
// a smaller DECK_LINK_MAX_FRAME knows what still fits.
func TestDocumentedLargestFrame(t *testing.T) {
	h, err := os.ReadFile(repo + "/firmware/deck-link/src/proto/link.pb.h")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`#define agentsdeck_link_v1_Frame_size\s+(\d+)`).FindSubmatch(h)
	if m == nil {
		t.Fatal("no Frame_size in link.pb.h")
	}
	n, _ := strconv.Atoi(string(m[1]))
	want := strconv.Itoa(n + 5)
	for _, path := range []string{"firmware/deck-link/src/deck_link.h", "firmware/deck-link/README.md", "docs/link/protocol.md"} {
		b, err := os.ReadFile(repo + "/" + path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), want+" bytes") && !strings.Contains(string(b), "("+want+")") {
			t.Errorf("%s doesn't give the largest frame as %s bytes", path, want)
		}
	}
}
