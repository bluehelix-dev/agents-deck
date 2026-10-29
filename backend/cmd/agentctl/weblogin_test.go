package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const testLoginURL = "http://127.0.0.1:7340/login?code=0123abcd&next=<x>"

func TestWriteLoginPage(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "web-login-old.html")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	long := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(stale, long, long); err != nil {
		t.Fatal(err)
	}
	// Another `agentctl web` running right now: its page must stay.
	busy := filepath.Join(dir, "web-login-busy.html")
	if err := os.WriteFile(busy, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "config.json")
	if err := os.WriteFile(keep, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	page, err := writeLoginPage(dir, testLoginURL)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(page) != dir || !strings.HasSuffix(page, ".html") {
		t.Errorf("page at %s", page)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a page from an earlier run was left behind")
	}
	if _, err := os.Stat(busy); err != nil {
		t.Error("removed the page of a login that is still going on")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("removed a file that is not a login page")
	}
	fi, err := os.Stat(page)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(page)
	s := string(b)
	esc := "http://127.0.0.1:7340/login?code=0123abcd&amp;next=&lt;x&gt;"
	if !strings.Contains(s, `<meta http-equiv="refresh" content="0;url=`+esc+`">`) {
		t.Errorf("no forward to the login URL:\n%s", s)
	}
	if strings.Contains(s, "<x>") {
		t.Error("the URL was not HTML-escaped")
	}
}

func TestOpenLoginURLKeepsCodeOffTheCommandLine(t *testing.T) {
	old := loginPageLinger
	loginPageLinger = 0
	t.Cleanup(func() { loginPageLinger = old })
	dir := t.TempDir()
	var opened, content string
	err := openLoginURL(dir, testLoginURL, func(arg string) error {
		opened = arg
		b, err := os.ReadFile(arg)
		content = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(opened, "0123abcd") || strings.Contains(opened, "code=") || filepath.Dir(opened) != dir {
		t.Errorf("the opener was given %q", opened)
	}
	if !strings.Contains(content, "0123abcd") {
		t.Error("the page the browser opens does not carry the code")
	}
	if _, err := os.Stat(opened); !os.IsNotExist(err) {
		t.Error("the login page was not removed")
	}

	// No opener (e.g. a headless machine): the page goes too, the caller prints the link.
	if err := openLoginURL(dir, testLoginURL, func(string) error { return errors.New("no browser") }); err == nil {
		t.Error("an opener failure was not reported")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}
