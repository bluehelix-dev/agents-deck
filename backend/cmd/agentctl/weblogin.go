package main

import (
	"html"
	"os"
	"path/filepath"
	"time"
)

// loginPageLinger is how long the login page stays on disk after the
// browser was asked to open it: long enough for a browser that is still
// starting to read it. Its code is single-use and expires within a minute.
var loginPageLinger = 10 * time.Second

const loginPagePattern = "web-login-*.html"

// openLoginURL opens loginURL (which holds the one-time login code) in the
// browser without putting it on any command line, where other users of the
// machine could read it (on Linux every process's arguments are public; only
// Linux uses this). The
// URL goes into a private page in dir (0600, in the 0700 data folder) that
// sends the browser on to it, and the browser is given the page's path
// instead. The page is removed after loginPageLinger.
func openLoginURL(dir, loginURL string, open func(string) error) error {
	page, err := writeLoginPage(dir, loginURL)
	if err != nil {
		return err
	}
	if err := open(page); err != nil {
		_ = os.Remove(page)
		return err
	}
	time.Sleep(loginPageLinger)
	_ = os.Remove(page)
	return nil
}

// staleLoginPage is the age after which a login page is surely left over:
// its code expired long ago. Younger pages may belong to an `agentctl web`
// running at the same moment, so they stay.
const staleLoginPage = 2 * time.Minute

// writeLoginPage writes the page that forwards to loginURL and returns its
// path. Stale pages left behind by an earlier run are removed first.
func writeLoginPage(dir, loginURL string) (string, error) {
	old, _ := filepath.Glob(filepath.Join(dir, loginPagePattern))
	for _, p := range old {
		if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) > staleLoginPage {
			_ = os.Remove(p)
		}
	}
	f, err := os.CreateTemp(dir, loginPagePattern) // created 0600
	if err != nil {
		return "", err
	}
	u := html.EscapeString(loginURL)
	_, err = f.WriteString("<!doctype html>\n<meta charset=\"utf-8\">\n<meta name=\"referrer\" content=\"no-referrer\">\n" +
		"<meta http-equiv=\"refresh\" content=\"0;url=" + u + "\">\n<title>agents deck</title>\n" +
		"<p>Opening the dashboard. If nothing happens, <a href=\"" + u + "\">open it here</a>.</p>\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
