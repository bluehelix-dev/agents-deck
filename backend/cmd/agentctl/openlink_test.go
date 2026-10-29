package main

import (
	"reflect"
	"runtime"
	"testing"
)

func TestLinkTarget(t *testing.T) {
	for _, c := range []struct{ link, word, want string }{
		{"https://example.com/a?b=1", "", "https://example.com/a?b=1"},
		{"file:///Users/sam/dev/app/main.go", "", "file:///Users/sam/dev/app/main.go"},
		{"", "(https://example.com/docs).", "https://example.com/docs"},
		{"", `"http://localhost:7340/x",`, "http://localhost:7340/x"},
		{"", "hello", ""},
		{"", "src/auth/session.ts", ""},
		{"", "", ""},
		{"javascript:alert(1)", "", ""},
		{"vscode://file/x", "", ""},
		{"https://", "", ""},
		{"", "ftp://example.com", ""},
		{"file://files.example/share/x", "", ""},
		{"file://localhost/Users/sam/notes.txt", "", "file://localhost/Users/sam/notes.txt"},
		{"https://example.com/a b", "", ""},
	} {
		if got := linkTarget(c.link, c.word); got != c.want {
			t.Errorf("linkTarget(%q, %q) = %q, want %q", c.link, c.word, got, c.want)
		}
	}
}

func TestLinkCommand(t *testing.T) {
	dirs := map[string]bool{"/home/sam/dev/app": true, "/home/sam/dev": true}
	isDir := func(p string) bool { return dirs[p] }
	for _, c := range []struct {
		goos, target string
		want         []string
	}{
		{"darwin", "", nil},
		{"darwin", "https://example.com/a", []string{"/usr/bin/open", "https://example.com/a"}},
		{"linux", "http://192.0.2.10:8080/", []string{"xdg-open", "http://192.0.2.10:8080/"}},
		{"windows", "https://example.com/a", []string{"rundll32", "url.dll,FileProtocolHandler", "https://example.com/a"}},
		// A file link is revealed, never opened: an app bundle or a script
		// must not launch from a click.
		{"darwin", "file:///Users/sam/dev/tools/report.command", []string{"/usr/bin/open", "-R", "/Users/sam/dev/tools/report.command"}},
		{"darwin", "file:///Users/sam/Downloads/Tool.app", []string{"/usr/bin/open", "-R", "/Users/sam/Downloads/Tool.app"}},
		{"darwin", "file:///Users/sam/My%20Notes/a.md", []string{"/usr/bin/open", "-R", "/Users/sam/My Notes/a.md"}},
		{"linux", "file:///home/sam/dev/app/run.desktop", []string{"xdg-open", "/home/sam/dev/app"}},
		{"linux", "file:///home/sam/dev/app", []string{"xdg-open", "/home/sam/dev"}},
		// The "folder" is a file: nothing opens.
		{"linux", "file:///home/sam/dev/evil.desktop/x", nil},
		{"linux", "javascript:alert(1)", nil},
		{"darwin", "vscode://file/x", nil},
	} {
		if got := linkCommand(c.goos, c.target, isDir); !reflect.DeepEqual(got, c.want) {
			t.Errorf("linkCommand(%q, %q) = %q, want %q", c.goos, c.target, got, c.want)
		}
	}
}

func TestLinkCommandWindowsFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows paths")
	}
	isDir := func(p string) bool { return p == `C:\work\app` }
	if got, want := linkCommand("windows", "file:///C:/work/app/run.bat", isDir), []string{"explorer.exe", `C:\work\app`}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := linkCommand("windows", "file:///C:/other/run.bat", isDir); got != nil {
		t.Errorf("got %q for a folder that isn't there", got)
	}
}
