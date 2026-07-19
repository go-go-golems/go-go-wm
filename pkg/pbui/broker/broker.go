// Package broker implements the PBUI broker: the daemon owning the accept
// state machine, the verb registry, and the event bus. It speaks the
// pbui wire protocol over a Unix socket and deliberately never links X —
// the window manager is just its most privileged client (design decision D2).
package broker

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

// DefaultSocketPath returns $PBUI_SOCKET, or $XDG_RUNTIME_DIR/pbui.sock,
// or a /tmp fallback.
func DefaultSocketPath() string {
	if p := os.Getenv("PBUI_SOCKET"); p != "" {
		return p
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "pbui.sock")
	}
	return fmt.Sprintf("/tmp/pbui-%d.sock", os.Getuid())
}

type clientID uint64

// conn is one connected client. The reader goroutine posts decoded frames
// to the broker loop; the writer goroutine drains send.
type conn struct {
	id    clientID
	name  string
	roles map[string]bool
	codec pbui.Codec
	raw   net.Conn
	send  chan *pbui.Msg
	done  chan struct{}
}

func (c *conn) enqueue(m *pbui.Msg) {
	select {
	case c.send <- m:
	case <-c.done:
	default:
		// Slow client: drop rather than stall the loop. Events are
		// best-effort; requests carry seqs and time out client-side.
	}
}

type acceptSession struct {
	id        string
	requester clientID
	seq       uint64 // requester's accept.start seq, echoed in accept.result
	ptypes    []string
	prompt    string
}

// Broker is the daemon. All state is owned by the run loop goroutine;
// connection goroutines communicate with it via posted closures.
type Broker struct {
	ops     chan func()
	clients map[clientID]*conn
	verbs   []pbui.Verb // registry; Owner names the registering client
	session *acceptSession
	subs    map[clientID]bool

	nextClient uint64
	nextSess   uint64
	eventSeq   uint64

	closed  atomic.Bool
	wg      sync.WaitGroup
	OnEvent func(event string, data []byte, source string) // optional embedded-mode tap
}

// New creates a broker (not yet serving).
func New() *Broker {
	return &Broker{
		ops:     make(chan func(), 256),
		clients: map[clientID]*conn{},
		subs:    map[clientID]bool{},
	}
}

// Serve accepts connections on l until ctx is cancelled. It runs the state
// loop in the calling goroutine; use go b.Serve(...) to run in background.
func (b *Broker) Serve(ctx context.Context, l net.Listener) error {
	defer b.closed.Store(true)

	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()

	// Acceptor: hands connections to the loop.
	acceptErr := make(chan error, 1)
	go func() {
		for {
			nc, err := l.Accept()
			if err != nil {
				acceptErr <- err
				return
			}
			b.post(func() { b.addConn(nc) })
		}
	}()

	// State loop.
	for {
		select {
		case fn := <-b.ops:
			fn()
		case err := <-acceptErr:
			// Drain remaining ops, close clients.
			b.post(nil)
			for fn := range b.ops {
				if fn == nil {
					break
				}
				fn()
			}
			for _, c := range b.clients {
				_ = c.raw.Close()
			}
			b.wg.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

// ListenAndServe listens on a Unix socket path (removing a stale socket
// file first) and serves until ctx is cancelled.
func (b *Broker) ListenAndServe(ctx context.Context, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Remove a stale socket if nothing is listening on it.
	if _, err := os.Stat(path); err == nil {
		if c, err := net.Dial("unix", path); err == nil {
			_ = c.Close()
			return fmt.Errorf("broker: socket %s is already in use", path)
		}
		_ = os.Remove(path)
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(path) }()
	return b.Serve(ctx, l)
}

func (b *Broker) post(fn func()) {
	if b.closed.Load() && fn != nil {
		return
	}
	b.ops <- fn
}

// --- loop-side handlers ----------------------------------------------------

func (b *Broker) addConn(nc net.Conn) {
	id := clientID(atomic.AddUint64(&b.nextClient, 1))
	c := &conn{
		id:    id,
		roles: map[string]bool{},
		codec: pbui.NewNDJSONCodec(nc),
		raw:   nc,
		send:  make(chan *pbui.Msg, 128),
		done:  make(chan struct{}),
	}
	b.clients[id] = c

	// Writer.
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case m := <-c.send:
				if err := c.codec.Encode(m); err != nil {
					_ = nc.Close()
					return
				}
			case <-c.done:
				return
			}
		}
	}()

	// Reader.
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			m, err := c.codec.Decode()
			if err != nil {
				b.post(func() { b.removeConn(c) })
				return
			}
			b.post(func() { b.handle(c, m) })
		}
	}()
}

func (b *Broker) removeConn(c *conn) {
	if _, ok := b.clients[c.id]; !ok {
		return
	}
	delete(b.clients, c.id)
	delete(b.subs, c.id)
	close(c.done)
	_ = c.raw.Close()
	// Drop the disconnected client's verbs.
	kept := b.verbs[:0]
	for _, v := range b.verbs {
		if v.Owner != c.name || c.name == "" {
			kept = append(kept, v)
		}
	}
	b.verbs = kept
	// Requester gone → session dies (rule 3, design doc §III.1).
	if b.session != nil && b.session.requester == c.id {
		b.clearSession("requester-disconnected")
	}
	b.emit("client.disconnected", jsonObj{"name": c.name}, "broker")
}

func (b *Broker) handle(c *conn, m *pbui.Msg) {
	switch m.T {
	case pbui.THello:
		c.name = m.Name
		for _, r := range m.Roles {
			c.roles[r] = true
		}
		c.enqueue(&pbui.Msg{T: pbui.TWelcome, Seq: m.Seq, Protocol: pbui.Protocol})
		b.emit("client.connected", jsonObj{"name": c.name, "roles": m.Roles}, "broker")
		// Late joiners see a pending accept immediately.
		if b.session != nil {
			c.enqueue(&pbui.Msg{
				T: pbui.TAcceptMode, Session: b.session.id,
				Ptypes: b.session.ptypes, Prompt: b.session.prompt,
			})
		}
	case pbui.TRegister:
		for _, v := range m.Verbs {
			v.Owner = c.name
			b.verbs = append(b.verbs, v)
		}
		c.enqueue(&pbui.Msg{T: pbui.TOK, Seq: m.Seq})
		b.emit("verbs.registered", jsonObj{"owner": c.name, "count": len(m.Verbs)}, "broker")
	case pbui.TSubscribe:
		b.subs[c.id] = true
		c.enqueue(&pbui.Msg{T: pbui.TOK, Seq: m.Seq})
	case pbui.TAcceptStart:
		// One session at a time: a new accept cancels the pending one
		// (matches the prototype overwriting `accepting`).
		if b.session != nil {
			b.clearSession("superseded")
		}
		if len(m.Ptypes) == 0 {
			c.enqueue(&pbui.Msg{T: pbui.TError, Seq: m.Seq, Code: "bad-request", Msg: "accept.start needs ptypes"})
			return
		}
		b.nextSess++
		s := &acceptSession{
			id:        fmt.Sprintf("s%d", b.nextSess),
			requester: c.id,
			seq:       m.Seq,
			ptypes:    m.Ptypes,
			prompt:    m.Prompt,
		}
		b.session = s
		b.broadcast(&pbui.Msg{T: pbui.TAcceptMode, Session: s.id, Ptypes: s.ptypes, Prompt: s.prompt})
		b.emit("accept.started", jsonObj{"session": s.id, "ptypes": s.ptypes, "prompt": s.prompt}, c.name)
	case pbui.TAcceptAnswer:
		s := b.session
		if s == nil || (m.Session != "" && m.Session != s.id) {
			c.enqueue(&pbui.Msg{T: pbui.TError, Seq: m.Seq, Code: "stale-answer", Msg: "no such accept session"})
			return
		}
		if m.Object == nil || !pbui.TypeMatches(s.ptypes, m.Object.Ptype) {
			c.enqueue(&pbui.Msg{T: pbui.TError, Seq: m.Seq, Code: "type-mismatch",
				Msg: fmt.Sprintf("session %s wants %v", s.id, s.ptypes)})
			return
		}
		if req, ok := b.clients[s.requester]; ok {
			req.enqueue(&pbui.Msg{T: pbui.TAcceptResult, Seq: s.seq, Session: s.id, Object: m.Object})
		}
		c.enqueue(&pbui.Msg{T: pbui.TOK, Seq: m.Seq})
		b.emit("accept.answered", jsonObj{"session": s.id, "ptype": m.Object.Ptype, "by": c.name}, c.name)
		b.session = nil
		b.broadcast(&pbui.Msg{T: pbui.TAcceptClear, Session: s.id, Reason: "done"})
	case pbui.TAcceptCancel:
		s := b.session
		if s == nil || (m.Session != "" && m.Session != s.id) {
			c.enqueue(&pbui.Msg{T: pbui.TOK, Seq: m.Seq}) // idempotent
			return
		}
		b.clearSession("cancelled")
		c.enqueue(&pbui.Msg{T: pbui.TOK, Seq: m.Seq})
	case pbui.TVerbInvoke:
		var owner *conn
		var verb *pbui.Verb
		for i := range b.verbs {
			if b.verbs[i].ID == m.VerbID {
				verb = &b.verbs[i]
				break
			}
		}
		if verb == nil {
			c.enqueue(&pbui.Msg{T: pbui.TError, Seq: m.Seq, Code: "no-verb", Msg: m.VerbID})
			return
		}
		for _, cl := range b.clients {
			if cl.name == verb.Owner {
				owner = cl
				break
			}
		}
		if owner == nil {
			c.enqueue(&pbui.Msg{T: pbui.TError, Seq: m.Seq, Code: "owner-gone", Msg: verb.Owner})
			return
		}
		owner.enqueue(&pbui.Msg{T: pbui.TVerbRun, VerbID: m.VerbID, Object: m.Object})
		c.enqueue(&pbui.Msg{T: pbui.TOK, Seq: m.Seq})
		b.emit("verb.invoked", jsonObj{"verb": m.VerbID, "by": c.name}, c.name)
	case pbui.TMenuRequest:
		if m.Object == nil {
			c.enqueue(&pbui.Msg{T: pbui.TError, Seq: m.Seq, Code: "bad-request", Msg: "menu.request needs object"})
			return
		}
		verbs := b.verbsFor(m.Object.Ptype)
		sent := false
		for _, cl := range b.clients {
			if cl.roles["wm"] {
				cl.enqueue(&pbui.Msg{T: pbui.TMenuShow, Object: m.Object, Verbs: verbs, X: m.X, Y: m.Y})
				sent = true
			}
		}
		if !sent {
			// No WM connected: return the verb list to the requester so
			// CLI clients can render a text menu.
			c.enqueue(&pbui.Msg{T: pbui.TVerbList, Seq: m.Seq, Verbs: verbs, Object: m.Object})
			return
		}
		c.enqueue(&pbui.Msg{T: pbui.TOK, Seq: m.Seq})
	case pbui.TQueryVerbs:
		ptype := ""
		if m.Object != nil {
			ptype = m.Object.Ptype
		}
		var verbs []pbui.Verb
		if ptype == "" {
			verbs = append(verbs, b.verbs...)
		} else {
			verbs = b.verbsFor(ptype)
		}
		c.enqueue(&pbui.Msg{T: pbui.TVerbList, Seq: m.Seq, Verbs: verbs})
	case pbui.TDocHover:
		for _, cl := range b.clients {
			if cl.roles["wm"] {
				cl.enqueue(&pbui.Msg{T: pbui.TDocHover, Text: m.Text, Source: c.name})
			}
		}
	case pbui.TEventEmit:
		b.emit(m.Event, m.Data, c.name)
		c.enqueue(&pbui.Msg{T: pbui.TOK, Seq: m.Seq})
	default:
		c.enqueue(&pbui.Msg{T: pbui.TError, Seq: m.Seq, Code: "unknown-type", Msg: m.T})
	}
}

func (b *Broker) verbsFor(ptype string) []pbui.Verb {
	var out []pbui.Verb
	for _, v := range b.verbs {
		if pbui.TypeMatches(v.Ptypes, ptype) {
			out = append(out, v)
		}
	}
	return out
}

func (b *Broker) clearSession(reason string) {
	s := b.session
	if s == nil {
		return
	}
	b.session = nil
	if req, ok := b.clients[s.requester]; ok {
		req.enqueue(&pbui.Msg{T: pbui.TAcceptResult, Seq: s.seq, Session: s.id, Object: nil, Reason: reason})
	}
	b.broadcast(&pbui.Msg{T: pbui.TAcceptClear, Session: s.id, Reason: reason})
	b.emit("accept.cleared", jsonObj{"session": s.id, "reason": reason}, "broker")
}

func (b *Broker) broadcast(m *pbui.Msg) {
	for _, c := range b.clients {
		c.enqueue(m)
	}
}

type jsonObj map[string]interface{}

func (b *Broker) emit(event string, data interface{}, source string) {
	b.eventSeq++
	raw := marshalData(data)
	m := &pbui.Msg{T: pbui.TEvent, EventSeq: b.eventSeq, Event: event, Data: raw, Source: source}
	for id := range b.subs {
		if c, ok := b.clients[id]; ok {
			c.enqueue(m)
		}
	}
	if b.OnEvent != nil {
		b.OnEvent(event, raw, source)
	}
}

func marshalData(data interface{}) []byte {
	switch d := data.(type) {
	case nil:
		return nil
	case []byte:
		return d
	default:
		raw, err := jsonMarshal(d)
		if err != nil {
			return nil
		}
		return raw
	}
}
