package wmx11

import (
	"bufio"
	"encoding/json"
	"net"
	"os"

	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// IPC: a tiny NDJSON query/control protocol on a Unix socket, in the spirit
// of bspc. "Ask the WM what it believes" is the test harness's assertion
// stream and the debugging tool forever (design doc §Part V).
//
// Requests:  {"q":"tree"} | {"q":"windows"} | {"q":"op","op":{...}}
// Responses: {"ok":true,"data":...} | {"ok":false,"error":"..."}

type ipcRequest struct {
	Q  string     `json:"q"`
	Op *wmcore.Op `json:"op,omitempty"`
}

type ipcResponse struct {
	OK    bool        `json:"ok"`
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
}

// WindowInfo is one row of {"q":"windows"}.
type WindowInfo struct {
	Leaf      string `json:"leaf"`
	Client    uint32 `json:"client"`
	Title     string `json:"title"`
	Workspace string `json:"workspace"`
	Rect      string `json:"rect"`
	Focused   bool   `json:"focused"`
}

func (w *WM) startIPC() error {
	path := w.cfg.IPCSocket
	if path == "" {
		path = DefaultIPCSocketPath()
	}
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	go func() {
		<-w.ctx.Done()
		_ = l.Close()
		_ = os.Remove(path)
	}()
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go w.serveIPC(conn)
		}
	}()
	return nil
}

func (w *WM) serveIPC(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	enc := json.NewEncoder(conn)
	for sc.Scan() {
		var req ipcRequest
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			_ = enc.Encode(ipcResponse{OK: false, Error: err.Error()})
			continue
		}
		resp := w.dispatchIPC(req)
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

// dispatchIPC runs the request on the WM loop and waits for the answer.
func (w *WM) dispatchIPC(req ipcRequest) ipcResponse {
	done := make(chan ipcResponse, 1)
	w.Post(func() {
		switch req.Q {
		case "tree":
			raw, err := w.desktop.Serialize()
			if err != nil {
				done <- ipcResponse{OK: false, Error: err.Error()}
				return
			}
			done <- ipcResponse{OK: true, Data: json.RawMessage(raw)}
		case "windows":
			var out []WindowInfo
			for leaf, f := range w.frames {
				info := WindowInfo{
					Leaf:    string(leaf),
					Client:  uint32(f.client),
					Title:   f.title,
					Rect:    f.rect.String(),
					Focused: w.focused == leaf,
				}
				if ws := w.desktop.FindLeafWorkspace(leaf); ws != nil {
					info.Workspace = ws.ID
				}
				out = append(out, info)
			}
			done <- ipcResponse{OK: true, Data: out}
		case "op":
			if req.Op == nil {
				done <- ipcResponse{OK: false, Error: "missing op"}
				return
			}
			res, err := w.Apply(*req.Op)
			if err != nil {
				done <- ipcResponse{OK: false, Error: err.Error()}
				return
			}
			done <- ipcResponse{OK: true, Data: res}
		default:
			done <- ipcResponse{OK: false, Error: "unknown query " + req.Q}
		}
	})
	select {
	case r := <-done:
		return r
	case <-w.ctx.Done():
		return ipcResponse{OK: false, Error: "wm shutting down"}
	}
}

// QueryIPC is the client side, used by the query CLI commands.
func QueryIPC(socket string, req interface{}, resp interface{}) error {
	if socket == "" {
		socket = DefaultIPCSocketPath()
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	return json.NewDecoder(bufio.NewReader(conn)).Decode(resp)
}
