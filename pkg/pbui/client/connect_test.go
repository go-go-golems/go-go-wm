package client

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A control-socket-like responder: replies to any line with a non-pbui
// frame (no "t"), exactly as the go-go-wm IPC socket answers a hello.
func fakeControlSocket(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ctl.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					_, _ = c.Write([]byte(`{"ok":false,"error":"unknown query hello"}` + "\n"))
				}
			}(c)
		}
	}()
	t.Cleanup(func() { _ = l.Close() })
	return path
}

func TestConnectToControlSocketGivesActionableError(t *testing.T) {
	path := fakeControlSocket(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Connect(ctx, Options{Socket: path, Name: "test"})
	if err == nil {
		t.Fatal("connecting to a non-pbui socket must fail")
	}
	if !strings.Contains(err.Error(), "not a pbui broker") ||
		!strings.Contains(err.Error(), "PBUI_SOCKET") {
		t.Fatalf("error must name the likely cause (wrong socket), got: %v", err)
	}
}

func TestConnectMissingSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Connect(ctx, Options{Socket: filepath.Join(os.TempDir(), "nope-does-not-exist.sock"), Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "connect") {
		t.Fatalf("missing socket must report a connect error, got: %v", err)
	}
}
