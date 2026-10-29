package main

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// linkCommand is the command that opens target (a link from linkTarget) on
// goos, or nil when nothing should run.
//
// Web links go to the system opener, as before. A file link could name an
// app bundle, a .command script, a .desktop file or anything else with a
// handler that runs code, so it is never handed to the opener as it is:
// macOS reveals it in Finder (open -R selects it, it launches nothing);
// elsewhere the folder that holds it opens in the file manager, and only
// when isDir confirms that folder is a real directory.
func linkCommand(goos, target string, isDir func(string) bool) []string {
	if target == "" {
		return nil
	}
	u, err := url.Parse(target)
	if err != nil {
		return nil
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		switch goos {
		case "darwin":
			return []string{"/usr/bin/open", target}
		case "windows":
			return []string{"rundll32", "url.dll,FileProtocolHandler", target}
		}
		return []string{"xdg-open", target}
	case "file":
	default:
		return nil
	}
	p := u.Path
	if !strings.HasPrefix(p, "/") {
		return nil
	}
	switch goos {
	case "darwin":
		return []string{"/usr/bin/open", "-R", p}
	case "windows":
		// file:///C:/dir/name.txt has the path /C:/dir/name.txt.
		p = strings.TrimPrefix(p, "/")
		if len(p) < 3 || p[1] != ':' {
			return nil
		}
		dir := filepath.Dir(filepath.FromSlash(p))
		if !isDir(dir) {
			return nil
		}
		return []string{"explorer.exe", dir}
	}
	dir := path.Dir(p)
	if !isDir(dir) {
		return nil
	}
	return []string{"xdg-open", dir}
}

// isDir reports whether p is a directory (after following links).
func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
