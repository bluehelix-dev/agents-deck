package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/pki"
)

// ---- network listener: TLS 1.3, one client certificate per device ----------

// NeedScope is the scope an API request needs: reading is view; acting on an
// agent is act; stopping, restarting or adding agents is admin.
func NeedScope(method, path string) devices.Scope {
	if method == http.MethodGet {
		if path == "/v1/live" {
			return devices.Admin // lists sessions outside the deck, to add them
		}
		return devices.View
	}
	switch {
	case strings.HasSuffix(path, "/dismiss"), strings.HasSuffix(path, "/resume"), strings.HasSuffix(path, "/handoff"),
		path == "/v1/restore", strings.HasPrefix(path, "/v1/live/"):
		return devices.Admin
	}
	return devices.Act
}

type deviceKey struct{}

// DeviceHandler serves the API to paired devices. The TLS layer has already
// checked the certificate against the CA and the registry (TLSConfig); here
// the device's scope gates each request. A connection without a certificate
// may only pair (POST /v1/pair with a one-time code).
func (s *Server) DeviceHandler() http.Handler {
	api := s.api()
	return secureHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, ok := s.deviceOf(r)
		if !ok {
			if r.Method == http.MethodPost && r.URL.Path == "/v1/pair" {
				s.pair(w, r)
				return
			}
			time.Sleep(300 * time.Millisecond) // slow down probing
			writeErr(w, http.StatusUnauthorized, "this device isn't paired: run `agentctl pair` on the Mac")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		if r.URL.Path == "/v1/link" && s.DeviceLink != nil {
			// A board's Deck Link session over Wi-Fi: a transport, not an API
			// endpoint. Its commands go through Dispatch (scope, audit).
			s.Devices.Seen(d.Name)
			s.DeviceLink(w, r, d)
			return
		}
		if need := NeedScope(r.Method, r.URL.Path); !d.Scope.Allows(need) {
			writeErr(w, http.StatusForbidden, d.Name+" may "+string(d.Scope)+", this needs "+string(need)+": agentctl devices scope "+d.Name+" "+string(need))
			return
		}
		s.Devices.Seen(d.Name)
		// The certificate serial rides along, so long-lived handlers
		// (GET /v1/events) can check the device again while they run.
		r = r.WithContext(context.WithValue(r.Context(), deviceKey{}, pki.SerialHex(r.TLS.VerifiedChains[0][0])))
		s.audited(d.Name, api).ServeHTTP(w, r)
	}))
}

// deviceOf is the active device whose verified certificate made this request.
func (s *Server) deviceOf(r *http.Request) (devices.Device, bool) {
	if s.Devices == nil || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
		return devices.Device{}, false
	}
	return s.Devices.ByCertSerial(pki.SerialHex(r.TLS.VerifiedChains[0][0]))
}

// stillPaired: the device that made this request is still active, that is,
// not revoked, removed or paired again with a new certificate since. A request
// without a device certificate (the unix socket, the web dashboard) always is.
func (s *Server) stillPaired(r *http.Request) bool {
	serial, ok := r.Context().Value(deviceKey{}).(string)
	if !ok {
		return true
	}
	if s.Devices == nil {
		return false
	}
	_, ok = s.Devices.ByCertSerial(serial)
	return ok
}

// TLSConfig: TLS 1.3 only; a client certificate is optional at the handshake
// (so a new device can pair) but must be signed by the deck's CA and belong
// to an active device when given; revoked devices fail the handshake.
func (s *Server) TLSConfig(cert tls.Certificate) *tls.Config {
	s.serverCert.Store(&cert)
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    s.CA.Pool(),
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.VerifiedChains) == 0 {
				return nil // no certificate: pairing only (DeviceHandler)
			}
			if _, ok := s.Devices.ByCertSerial(pki.SerialHex(cs.VerifiedChains[0][0])); !ok {
				return errors.New("device revoked or unknown")
			}
			return nil
		},
	}
	// Each handshake takes the certificate SetServerCert installed last: it
	// follows the computer's addresses while the listener runs. (Through
	// GetConfigForClient, not GetCertificate: devices connect by address,
	// without a server name, and GetCertificate is skipped then.)
	cfg.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		c := cfg.Clone()
		c.Certificates = []tls.Certificate{*s.serverCert.Load()}
		c.GetConfigForClient = nil
		return c, nil
	}
	return cfg
}

// SetServerCert installs the network listener's certificate: what the
// listener presents from now on, its fingerprint for boards and bundles, and
// hosts, where boards on Wi-Fi reach it (the computer's name, then its
// addresses).
func (s *Server) SetServerCert(cert tls.Certificate, hosts []string) {
	s.certMu.Lock()
	defer s.certMu.Unlock()
	s.serverCert.Store(&cert)
	s.ServerFingerprint = pki.Fingerprint(cert.Certificate[0])
	s.DeviceHosts = hosts
}

// ServerFP is the listener certificate's fingerprint, "sha256:…".
func (s *Server) ServerFP() string {
	s.certMu.RLock()
	defer s.certMu.RUnlock()
	return s.ServerFingerprint
}

// Hosts is where boards reach the listener: the computer's name, then its
// addresses.
func (s *Server) Hosts() []string {
	s.certMu.RLock()
	defer s.certMu.RUnlock()
	return s.DeviceHosts
}

// WatchServerCert keeps the listener's certificate covering the computer's
// addresses: every `every` it runs RefreshServerCert with what hosts() says
// now. A VPN that comes up after the daemon, another Wi-Fi: a device that
// connects by the new address then verifies the certificate against the
// CA, instead of falling back to the certificate it paired with (which a
// reissue since would have changed).
func (s *Server) WatchServerCert(ctx context.Context, certPath, keyPath string,
	hosts func() (certHosts, boardHosts []string), every time.Duration, logf func(string, ...any)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		certHosts, boardHosts := hosts()
		s.RefreshServerCert(certPath, keyPath, certHosts, boardHosts, logf)
	}
}

// RefreshServerCert is one round of WatchServerCert: the certificate on
// disk when it still covers certHosts, a new one from the CA otherwise,
// installed when it differs from the one served now. An address that went
// away changes nothing; one that appeared means a new certificate.
func (s *Server) RefreshServerCert(certPath, keyPath string, certHosts, boardHosts []string, logf func(string, ...any)) {
	cert, err := s.CA.ServerCert(certPath, keyPath, certHosts)
	if err != nil {
		logf("server certificate: %v", err)
		return
	}
	if cur := s.serverCert.Load(); cur != nil && bytes.Equal(cur.Certificate[0], cert.Certificate[0]) {
		s.certMu.Lock()
		s.DeviceHosts = boardHosts
		s.certMu.Unlock()
		return
	}
	s.SetServerCert(cert, boardHosts)
	logf("server certificate reissued: it covers %d names and addresses now", len(certHosts))
}

// ServeDevice runs the network listener.
func ServeDevice(addr string, cfg *tls.Config, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		TLSConfig:         cfg,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          log.New(handshakeNoise{next: func(s string) { log.Print(s) }}, "", 0),
	}
	return srv.ListenAndServeTLS("", "")
}

// handshakeNoise drops net/http's "TLS handshake error" lines (devices
// reconnecting make plenty) and passes every other line on, such as a
// handler's panic or an Accept error.
type handshakeNoise struct{ next func(string) }

func (h handshakeNoise) Write(p []byte) (int, error) {
	if !bytes.HasPrefix(p, []byte("http: TLS handshake error")) {
		h.next(strings.TrimSuffix(string(p), "\n"))
	}
	return len(p), nil
}

// pair: POST /v1/pair {"code", "csr"} → {"name", "cert", "ca"}. The code
// (from `agentctl pair <name>`, 2 minutes, single use) names the device and
// its scope; the request's key becomes the device's.
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
		CSR  string `json:"csr"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16384)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json")
		return
	}
	name, scope, ok := s.Devices.RedeemPairingCode(body.Code, equal)
	if !ok {
		time.Sleep(300 * time.Millisecond)
		writeErr(w, http.StatusUnauthorized, "pairing code expired or wrong: run `agentctl pair` again")
		return
	}
	cert, certPEM, err := s.CA.SignCSR(name, []byte(body.CSR))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Devices.AddNetwork(name, scope, pki.SerialHex(cert), pki.Fingerprint(cert.Raw)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Audit.Log(devices.AuditEntry{Device: name, Action: "pair", Status: http.StatusOK, Detail: "scope " + string(scope)})
	log.Printf("paired %s (%s)", name, scope)
	s.Store.Touch() // open event streams check their device again: the name's old certificate is gone
	writeJSON(w, http.StatusOK, map[string]string{"name": name, "cert": string(certPEM), "ca": string(s.CA.PEM)})
}

// Dispatch runs one API request for a device that isn't on the network
// listener (a Deck Link board): the same scope check, handlers and audit log.
// It returns the status code and the response body.
func (s *Server) Dispatch(d devices.Device, method, path string, body []byte) (int, []byte) {
	if need := NeedScope(method, path); !d.Scope.Allows(need) {
		b, _ := json.Marshal(map[string]string{"error": d.Name + " may " + string(d.Scope) + ", this needs " + string(need)})
		return http.StatusForbidden, b
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.audited(d.Name, s.sharedAPI()).ServeHTTP(w, r)
	if s.Devices != nil {
		s.Devices.Seen(d.Name)
	}
	return w.Code, w.Body.Bytes()
}

// ---- audit -------------------------------------------------------------------

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) { w.code = c; w.ResponseWriter.WriteHeader(c) }
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// audited logs every request that changes something, under who.
func (s *Server) audited(who string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || s.Audit == nil {
			h.ServeHTTP(w, r)
			return
		}
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		h.ServeHTTP(sw, r)
		s.Audit.Log(devices.AuditEntry{Device: who, Action: r.Method + " " + r.URL.Path, Status: sw.code})
	})
}

// ---- device management on the unix socket (agentctl devices / pair) ----------

// Bundle is what a provisioned device needs to connect: where, whom to trust,
// and its own certificate and key.
type Bundle struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	CA          string `json:"ca"`          // PEM: the deck's CA
	Fingerprint string `json:"fingerprint"` // the listener's certificate, for hosts its names don't cover
	Cert        string `json:"cert"`        // PEM: the device's certificate
	Key         string `json:"key"`         // PEM: the device's private key
}

func (s *Server) localDevices(mux *http.ServeMux) {
	ready := func(w http.ResponseWriter) bool {
		if s.Devices == nil || s.CA == nil {
			writeErr(w, http.StatusServiceUnavailable, "the network listener is off (deviceAddr in config.json)")
			return false
		}
		return true
	}
	mux.HandleFunc("GET /local/devices", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"devices": s.Devices.List(), "ca": s.CA.Fingerprint(), "url": s.DeviceURL,
			"autoApprove": s.Devices.Settings().AutoApprove})
	})
	// Approving boards plugged in over USB by themselves: on or off, and the scope they get.
	mux.HandleFunc("POST /local/devices/auto-approve", func(w http.ResponseWriter, r *http.Request) {
		if s.Devices == nil {
			writeErr(w, http.StatusServiceUnavailable, "no device registry")
			return
		}
		var b struct {
			USB   *bool  `json:"usb"`
			Scope string `json:"scope"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&b); err != nil || b.USB == nil {
			writeErr(w, http.StatusBadRequest, `body must be {"usb": true|false, "scope": "view"|"act"}`)
			return
		}
		sc := s.Devices.Settings().AutoApprove.Scope
		if b.Scope != "" {
			sc = devices.Scope(b.Scope)
		}
		if err := s.Devices.SetAutoApprove(*b.USB, sc); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		state := "off"
		if *b.USB {
			state = "on, scope " + string(sc)
		}
		s.Audit.Log(devices.AuditEntry{Device: "local", Action: "auto-approve usb", Status: http.StatusOK, Detail: state})
		writeJSON(w, http.StatusOK, s.Devices.Settings().AutoApprove)
	})
	type nameScope struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	read := func(w http.ResponseWriter, r *http.Request) (string, devices.Scope, bool) {
		var b nameScope
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b); err != nil {
			writeErr(w, http.StatusBadRequest, "bad json")
			return "", "", false
		}
		if b.Scope == "" {
			b.Scope = string(devices.Act)
		}
		sc, err := devices.ParseScope(b.Scope)
		if err == nil {
			err = devices.ValidName(b.Name)
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return "", "", false
		}
		return b.Name, sc, true
	}
	// A one-time code for a device that makes its own key (POST /v1/pair).
	mux.HandleFunc("POST /local/devices/code", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		name, sc, ok := read(w, r)
		if !ok {
			return
		}
		code := RandomToken()[:20]
		if err := s.Devices.NewPairingCode(name, sc, code); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"code": code, "url": s.DeviceURL, "ca": s.CA.Fingerprint(),
			"expires": time.Now().Add(devices.PairingCodeTTL).UTC().Format(time.RFC3339)})
	})
	// A key and certificate made here, for a device you set up over SSH.
	mux.HandleFunc("POST /local/devices/provision", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		name, sc, ok := read(w, r)
		if !ok {
			return
		}
		cert, certPEM, keyPEM, err := s.CA.NewDeviceKey(name)
		if err == nil {
			err = s.Devices.AddNetwork(name, sc, pki.SerialHex(cert), pki.Fingerprint(cert.Raw))
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		s.Audit.Log(devices.AuditEntry{Device: "local", Action: "provision " + name, Status: http.StatusOK, Detail: "scope " + string(sc)})
		s.Store.Touch() // open event streams check their device again: the name's old certificate is gone
		writeJSON(w, http.StatusOK, Bundle{Name: name, URL: s.DeviceURL, CA: string(s.CA.PEM), Fingerprint: s.ServerFP(),
			Cert: string(certPEM), Key: string(keyPEM)})
	})
	mux.HandleFunc("POST /local/devices/{name}/{action}", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		name := r.PathValue("name")
		var err error
		switch r.PathValue("action") {
		case "revoke":
			err = s.Devices.Revoke(name)
		case "approve":
			err = s.Devices.Approve(name)
		case "repair": // a board pairs again over USB (a new key and certificate)
			err = s.Devices.ClearBoardCert(name)
		case "remove":
			err = s.Devices.Remove(name)
		case "scope":
			var b nameScope
			_ = json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&b)
			var sc devices.Scope
			if sc, err = devices.ParseScope(b.Scope); err == nil {
				err = s.Devices.SetScope(name, sc)
			}
		default:
			writeErr(w, http.StatusNotFound, "unknown action")
			return
		}
		switch {
		case errors.Is(err, devices.ErrUnknown):
			writeErr(w, http.StatusNotFound, err.Error())
		case err != nil:
			writeErr(w, http.StatusBadRequest, err.Error())
		default:
			s.Audit.Log(devices.AuditEntry{Device: "local", Action: r.PathValue("action") + " " + name, Status: http.StatusNoContent})
			s.Store.Touch() // the devices list on the web dashboard
			w.WriteHeader(http.StatusNoContent)
		}
	})
}

// ---- Deck Link boards: Wi-Fi certificates and credentials --------------------

// PairBoard issues an approved serial board its client certificate from a
// CSR (DER or PEM) for its own key, and records it on the board's entry: the
// same name, scope, revocation and audit log over Wi-Fi as over USB. It
// returns the certificate (DER) and its serial, or an HTTP status and error.
func (s *Server) PairBoard(d devices.Device, csr []byte) (cert []byte, serial string, code int, err error) {
	if s.CA == nil || s.Devices == nil {
		return nil, "", http.StatusServiceUnavailable, errors.New("the network listener is off (deviceAddr in config.json)")
	}
	if d.Kind != devices.Serial || !d.Active() {
		return nil, "", http.StatusForbidden, errors.New(d.Name + " isn't approved: agentctl devices approve " + d.Name)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(csr), []byte("-----BEGIN")) {
		csr = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr})
	}
	c, _, err := s.CA.SignCSR(d.Name, csr)
	if err != nil {
		return nil, "", http.StatusBadRequest, err
	}
	serial = pki.SerialHex(c)
	if err := s.Devices.SetBoardCert(d.Name, serial, pki.Fingerprint(c.Raw)); err != nil {
		return nil, "", http.StatusForbidden, err
	}
	s.Audit.Log(devices.AuditEntry{Device: d.Name, Action: "pair (deck link)", Status: http.StatusOK, Detail: "certificate " + serial})
	log.Printf("link: %s paired for Wi-Fi (certificate %s)", d.Name, serial)
	s.Store.Touch() // the devices list on the web dashboard
	return c.Raw, serial, http.StatusOK, nil
}

// linkWifi: POST /local/link/wifi {"device", "ssid", "password", "forget"}.
// Unix socket only: the password goes straight to the board over USB serial
// and is never stored, logged or audited on the Mac. There is deliberately no
// /v1 counterpart: /v1 is reachable from the network, and a Wi-Fi password
// shouldn't travel further than this Mac and the board's USB cable.
func (s *Server) linkWifi(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Device     string `json:"device"`
		SSID       string `json:"ssid"`
		Password   string `json:"password"`
		Forget     bool   `json:"forget"`
		UseDefault bool   `json:"useDefault"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b); err != nil || b.Device == "" {
		writeErr(w, http.StatusBadRequest, `body must be {"device", "ssid", "password"}, {"device", "forget": true} or {"device", "useDefault": true}`)
		return
	}
	if b.UseDefault {
		s.linkWifiUseDefault(w, b.Device)
		return
	}
	if !b.Forget {
		if err := CheckWifi(b.SSID, b.Password); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if s.LinkWifi == nil {
		writeErr(w, http.StatusServiceUnavailable, "Deck Link is off")
		return
	}
	code, msg := s.LinkWifi(b.Device, b.SSID, b.Password, b.Forget)
	what := "set"
	if b.Forget {
		what = "forget"
	}
	if s.Audit != nil {
		s.Audit.Log(devices.AuditEntry{Device: "local", Action: "wifi " + b.Device, Status: code, Detail: what})
	}
	if code >= 300 {
		writeErr(w, code, msg)
		return
	}
	// Its own network (or none): the default network leaves it alone.
	if s.Devices != nil {
		if err := s.Devices.SetWifiOwn(b.Device, true); err != nil {
			log.Printf("wifi %s: %v", b.Device, err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// linkWifiUseDefault: the board follows the default network again; on USB
// right now, it gets it at once. Answers {"sent", "message"}.
func (s *Server) linkWifiUseDefault(w http.ResponseWriter, device string) {
	if s.Devices == nil {
		writeErr(w, http.StatusServiceUnavailable, "no device registry")
		return
	}
	if err := s.Devices.SetWifiOwn(device, false); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, devices.ErrUnknown) {
			code = http.StatusNotFound
		}
		writeErr(w, code, err.Error())
		return
	}
	s.Audit.Log(devices.AuditEntry{Device: "local", Action: "wifi " + device, Status: http.StatusOK, Detail: "use default"})
	sent, msg := false, "it gets the default network next time it is plugged in"
	if s.LinkWifiUseDefault != nil {
		code, m := s.LinkWifiUseDefault(device)
		switch {
		case code < 300:
			sent, msg = true, "sent the default network: it is testing it"
		case code == http.StatusConflict || code == http.StatusNotFound:
			msg = m
		default:
			writeErr(w, code, m)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent, "message": msg})
}

// linkWifiDefault: POST /local/link/wifi-default {"ssid", "password"} or
// {"forget": true}. Unix socket only, like linkWifi: the password goes to the
// OS secret store (the Keychain) and to boards over USB, and is never logged
// or audited.
func (s *Server) linkWifiDefault(w http.ResponseWriter, r *http.Request) {
	var b struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
		Forget   bool   `json:"forget"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b); err != nil {
		writeErr(w, http.StatusBadRequest, `body must be {"ssid", "password"} or {"forget": true}`)
		return
	}
	if !b.Forget {
		if err := CheckWifi(b.SSID, b.Password); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if s.LinkWifiDefault == nil {
		writeErr(w, http.StatusServiceUnavailable, "Deck Link is off")
		return
	}
	what := "wifi default set"
	if b.Forget {
		what = "wifi default cleared"
	}
	if err := s.LinkWifiDefault(b.SSID, b.Password, b.Forget); err != nil {
		s.Audit.Log(devices.AuditEntry{Device: "local", Action: what, Status: http.StatusInternalServerError})
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Audit.Log(devices.AuditEntry{Device: "local", Action: what, Status: http.StatusNoContent})
	w.WriteHeader(http.StatusNoContent)
}

// CheckWifi: an SSID of 1 to 32 bytes; a WPA passphrase of 8 to 63
// characters, 64 hex digits, or "" for an open network.
func CheckWifi(ssid, password string) error {
	if ssid == "" || len(ssid) > 32 {
		return errors.New("the network name (SSID) must be 1 to 32 bytes")
	}
	switch n := len(password); {
	case n == 0, n >= 8 && n <= 63:
	case n == 64 && isHex(password):
	default:
		return errors.New("a Wi-Fi password has 8 to 63 characters (or 64 hex digits); leave it empty for an open network")
	}
	return nil
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
