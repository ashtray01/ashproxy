package engine

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestParentAndBackup(t *testing.T) {
	exe, _ := filepath.Abs("../core/3proxy.exe")
	parent := New(exe, t.TempDir())
	t.Cleanup(parent.Close)
	c := defaults()
	c.SOCKS = freePort(t)
	c.HTTP = 0
	if err := parent.Save(c); err != nil {
		t.Fatal(err)
	}
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	child := New(exe, t.TempDir())
	t.Cleanup(child.Close)
	c = defaults()
	c.SOCKS = 0
	c.HTTP = freePort(t)
	if err := child.Save(c); err != nil {
		t.Fatal(err)
	}
	c.ParentType = "socks5"
	c.ParentAddr = "127.0.0.1:" + strconv.Itoa(parent.Configuration().SOCKS)
	if err := child.Save(c); err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "proxied") }))
	defer dest.Close()
	proxy := &url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(c.HTTP)}
	tr := &http.Transport{Proxy: http.ProxyURL(proxy)}
	defer tr.CloseIdleConnections()
	client := http.Client{Transport: tr, Timeout: 4 * time.Second}
	r, err := client.Get(dest.URL)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil || string(data) != "proxied" {
		t.Fatal("parent request failed")
	}
	deadline := time.Now().Add(3 * time.Second)
	for parent.Status().RX == 0 {
		if time.Now().After(deadline) {
			t.Fatal("parent received no traffic")
		}
		time.Sleep(80 * time.Millisecond)
	}
	backups, err := child.Backups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups %v: %v", backups, err)
	}
	if err = child.Restore(backups[0]); err != nil {
		t.Fatal(err)
	}
	if child.Configuration().ParentType != "" || child.Status().State != "running" {
		t.Fatal("restore did not reapply configuration")
	}
}
