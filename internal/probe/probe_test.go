package probe_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/relayx-tech/agent/internal/probe"
)

func TestTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	ok, latency, msg := probe.TCP(context.Background(), ln.Addr().String(), time.Second)
	if !ok || msg != "" {
		t.Fatalf("ok=%v msg=%q latency=%d", ok, msg, latency)
	}
}

func TestTCPFailure(t *testing.T) {
	ok, _, msg := probe.TCP(context.Background(), "127.0.0.1:1", 200*time.Millisecond)
	if ok || msg == "" {
		t.Fatalf("ok=%v msg=%q", ok, msg)
	}
}

func TestHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ok, _, code, msg := probe.HTTP(context.Background(), srv.URL, time.Second)
	if !ok || code != http.StatusOK || msg != "" {
		t.Fatalf("ok=%v code=%d msg=%q", ok, code, msg)
	}
}
