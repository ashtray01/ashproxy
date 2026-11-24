package engine

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
func TestProtocolsAndLifecycle(t *testing.T) {
	exe, _ := filepath.Abs("../core/3proxy.exe")
	if _, err := os.Stat(exe); err != nil {
		t.Fatal("Build the desktop core before integration tests:", err)
	}
	m := New(exe, filepath.Join(t.TempDir(), "данные с пробелами"))
	c := defaults()
	c.Allowed = "192.168.90.0/23, 127.0.0.1/32"
	c.HTTP = freePort(t)
	c.SOCKS = freePort(t)
	if err := m.Save(c); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "through ashproxy") })
	origin := httptest.NewServer(handler)
	defer origin.Close()
	secure := httptest.NewTLSServer(handler)
	defer secure.Close()
	for _, tt := range []struct {
		scheme string
		port   int
		dest   string
	}{{"http", c.HTTP, origin.URL}, {"http", c.HTTP, secure.URL}, {"socks5", c.SOCKS, origin.URL}} {
		u, _ := url.Parse(tt.scheme + "://127.0.0.1:" + strconv.Itoa(tt.port))
		tr := &http.Transport{Proxy: http.ProxyURL(u), TLSClientConfig: &tls.Config{RootCAs: secure.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs}}
		client := http.Client{Transport: tr, Timeout: 4 * time.Second}
		resp, err := client.Get(tt.dest)
		if err != nil {
			t.Fatal(tt, err)
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		tr.CloseIdleConnections()
		if err != nil || string(b) != "through ashproxy" {
			t.Fatalf("%v returned %q, %v", tt, b, err)
		}
	}
	conn, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(c.SOCKS))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	deadline := time.Now().Add(4 * time.Second)
	for {
		v := m.Status()
		if v.State == "running" && len(v.Clients) > 0 && v.RX > 0 && v.TX > 0 && len(v.History) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("missing telemetry: %+v", v)
		}
		time.Sleep(50 * time.Millisecond)
	}
	conn.Close()
	if stats, err := m.Stats(); err != nil || stats.Total.RX == 0 {
		t.Fatalf("stats while running: %+v %v", stats, err)
	}
	if v := m.Status(); v.Totals.RX == 0 || v.Totals.TX == 0 {
		t.Fatalf("status without lifetime totals: %+v", v)
	}
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	bad := c
	bad.HTTP = occupied.Addr().(*net.TCPAddr).Port
	if err = m.Save(bad); err == nil {
		t.Fatal("accepted occupied port")
	}
	if v := m.Status(); v.Config != c || v.State != "running" {
		t.Fatalf("rollback failed: %+v", v)
	}
	if err = m.Restart(); err != nil {
		t.Fatal(err)
	}
	if err = m.Stop(); err != nil {
		t.Fatal(err)
	}
	if m.Status().State != "stopped" {
		t.Fatal("still running")
	}
	checked := New(exe, m.dir)
	t.Cleanup(checked.Close)
	if checked.Configuration() != c {
		t.Fatal("configuration did not persist")
	}
	points, err := checked.Traffic(60)
	if err != nil {
		t.Fatal(err)
	}
	var received float64
	for _, p := range points {
		received += p.RX
	}
	if received == 0 {
		t.Fatal("history did not survive a new manager")
	}
	stats, err := checked.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total.RX == 0 || stats.Total.TX == 0 || len(stats.Days) == 0 || len(stats.Months) == 0 {
		t.Fatalf("missing lifetime totals: %+v", stats)
	}
	if stats.Days[0].Label == "" || stats.Months[0].Label == "" {
		t.Fatalf("missing labels: %+v", stats)
	}
	if err = checked.ResetTraffic(); err != nil {
		t.Fatal(err)
	}
	after, err := checked.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if after.Total.RX != 0 || after.Total.TX != 0 || after.Total.Since == 0 || len(after.Days) != 0 || len(after.Months) != 0 {
		t.Fatalf("reset left data: %+v", after)
	}
	if points, err = checked.Traffic(60); err != nil {
		t.Fatal(err)
	}
	for _, p := range points {
		if p.RX != 0 || p.TX != 0 {
			t.Fatal("reset left minute samples")
		}
	}
}
func TestProxyCredentials(t *testing.T) {
	exe, _ := filepath.Abs("../core/3proxy.exe")
	m := New(exe, t.TempDir())
	t.Cleanup(m.Close)
	c := defaults()
	c.Allowed = "0.0.0.0/0"
	c.HTTP = freePort(t)
	c.SOCKS = 0
	if err := m.Save(c); err != nil {
		t.Fatal(err)
	}
	password := strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := m.SetCredential("operator", password); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer origin.Close()
	for _, authorized := range []bool{false, true} {
		u := &url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(c.HTTP)}
		if authorized {
			u.User = url.UserPassword("operator", password)
		}
		tr := &http.Transport{Proxy: http.ProxyURL(u)}
		client := http.Client{Transport: tr, Timeout: 4 * time.Second}
		r, err := client.Get(origin.URL)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		tr.CloseIdleConnections()
		if authorized && r.StatusCode != 204 || !authorized && r.StatusCode != 407 {
			t.Fatalf("authorized=%t status=%d", authorized, r.StatusCode)
		}
	}
	for _, name := range []string{"settings.json", "runtime.cfg"} {
		data, err := os.ReadFile(filepath.Join(m.dir, name))
		if err != nil || strings.Contains(string(data), password) {
			t.Fatalf("credential storage: %s", name)
		}
	}
}
func TestInvalidSettings(t *testing.T) {
	c := defaults()
	for _, acl := range []string{"192.168.1.0/33", "localhost", "127.0.0.1/32\nadmin", "2001:db8::/32", ""} {
		c.Allowed = acl
		if c.validate() == nil {
			t.Fatal(acl)
		}
	}
	for _, acl := range []string{"0.0.0.0/0", "192.168.90.0/23", "10.0.0.0/7", "127.0.0.1, 192.168.90.0/23\n10.1.2.3"} {
		c = defaults()
		c.Allowed = acl
		if err := c.validate(); err != nil {
			t.Fatal(acl, err)
		}
	}
	c = defaults()
	c.Allowed = "192.168.90.5/23, 127.0.0.1"
	if got := c.normalize().Allowed; got != "192.168.90.0/23, 127.0.0.1/32" {
		t.Fatal(got)
	}
	if got := c.normalize().render(); !strings.Contains(got, "allow * 192.168.90.0/23\nallow * 127.0.0.1/32\n") {
		t.Fatal(got)
	}
	c = defaults()
	c.Allowed = "0.0.0.0/0"
	if !strings.Contains(c.render(), "allow * 0.0.0.0/0\n") {
		t.Fatal(c.render())
	}
	c = defaults()
	c.HTTP = c.SOCKS
	if c.validate() == nil {
		t.Fatal("port collision")
	}
	c = defaults()
	c.HTTP = 0
	if strings.Contains(c.render(), "proxy -4") {
		t.Fatal("disabled HTTP listener")
	}
}
