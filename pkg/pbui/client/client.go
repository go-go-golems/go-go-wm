// Package client is the Go client for the PBUI broker: what applications
// (and the window manager itself) link against to present objects, accept
// them, register verbs, and follow the event bus.
package client

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/broker"
)

// Client is one broker connection. Safe for concurrent use.
type Client struct {
	conn  net.Conn
	codec pbui.Codec

	mu      sync.Mutex
	seq     uint64
	pending map[uint64]chan *pbui.Msg
	events  chan *pbui.Msg
	// handlers for unsolicited broker → client messages
	onAcceptMode  func(session string, ptypes []string, prompt string)
	onAcceptClear func(session, reason string)
	onVerbRun     func(verbID string, obj *pbui.Object)
	onMenuShow    func(obj pbui.Object, verbs []pbui.Verb, x, y int)
	onDocHover    func(text, source string)

	readErr  atomic.Value // error
	closed   chan struct{}
	closeOne sync.Once
	name     string // announced in hello; verb/command ownership key
}

// Options configures Connect.
type Options struct {
	Socket string   // empty → broker.DefaultSocketPath()
	Name   string   // client name announced in hello (verb ownership key)
	Roles  []string // e.g. ["app"], ["wm"], ["picker"]
}

// Connect dials the broker and performs the hello handshake.
func Connect(ctx context.Context, opts Options) (*Client, error) {
	path := opts.Socket
	if path == "" {
		path = broker.DefaultSocketPath()
	}
	var d net.Dialer
	nc, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("pbui: connect %s: %w", path, err)
	}
	c := &Client{
		conn:    nc,
		codec:   pbui.NewNDJSONCodec(nc),
		pending: map[uint64]chan *pbui.Msg{},
		events:  make(chan *pbui.Msg, 256),
		name:    opts.Name,
		closed:  make(chan struct{}),
	}
	go c.readLoop()
	roles := opts.Roles
	if len(roles) == 0 {
		roles = []string{"app"}
	}
	if _, err := c.request(ctx, &pbui.Msg{T: pbui.THello, Name: opts.Name, Roles: roles, Protocol: pbui.Protocol}); err != nil {
		_ = nc.Close()
		return nil, err
	}
	return c, nil
}

// Close tears down the connection.
func (c *Client) Close() error {
	c.closeOne.Do(func() { close(c.closed) })
	return c.conn.Close()
}

// Done is closed when the connection dies.
func (c *Client) Done() <-chan struct{} { return c.closed }

// --- handler registration (call before the relevant traffic starts) --------

func (c *Client) OnAcceptMode(fn func(session string, ptypes []string, prompt string)) {
	c.mu.Lock()
	c.onAcceptMode = fn
	c.mu.Unlock()
}
func (c *Client) OnAcceptClear(fn func(session, reason string)) {
	c.mu.Lock()
	c.onAcceptClear = fn
	c.mu.Unlock()
}
func (c *Client) OnVerbRun(fn func(verbID string, obj *pbui.Object)) {
	c.mu.Lock()
	c.onVerbRun = fn
	c.mu.Unlock()
}
func (c *Client) OnMenuShow(fn func(obj pbui.Object, verbs []pbui.Verb, x, y int)) {
	c.mu.Lock()
	c.onMenuShow = fn
	c.mu.Unlock()
}
func (c *Client) OnDocHover(fn func(text, source string)) {
	c.mu.Lock()
	c.onDocHover = fn
	c.mu.Unlock()
}

func (c *Client) readLoop() {
	for {
		m, err := c.codec.Decode()
		if err != nil {
			c.readErr.Store(err)
			c.closeOne.Do(func() { close(c.closed) })
			c.mu.Lock()
			for seq, ch := range c.pending {
				close(ch)
				delete(c.pending, seq)
			}
			c.mu.Unlock()
			close(c.events)
			return
		}
		switch m.T {
		case pbui.TEvent:
			select {
			case c.events <- m:
			default: // slow consumer: drop
			}
		case pbui.TAcceptMode:
			c.mu.Lock()
			fn := c.onAcceptMode
			c.mu.Unlock()
			if fn != nil {
				fn(m.Session, m.Ptypes, m.Prompt)
			}
		case pbui.TAcceptClear:
			c.mu.Lock()
			fn := c.onAcceptClear
			c.mu.Unlock()
			if fn != nil {
				fn(m.Session, m.Reason)
			}
		case pbui.TVerbRun:
			c.mu.Lock()
			fn := c.onVerbRun
			c.mu.Unlock()
			if fn != nil {
				fn(m.VerbID, m.Object)
			}
		case pbui.TMenuShow:
			c.mu.Lock()
			fn := c.onMenuShow
			c.mu.Unlock()
			if fn != nil && m.Object != nil {
				fn(*m.Object, m.Verbs, m.X, m.Y)
			}
		case pbui.TDocHover:
			c.mu.Lock()
			fn := c.onDocHover
			c.mu.Unlock()
			if fn != nil {
				fn(m.Text, m.Source)
			}
		default:
			// Response to a pending request.
			c.mu.Lock()
			ch := c.pending[m.Seq]
			delete(c.pending, m.Seq)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
}

// request sends m with a fresh seq and waits for the matching reply.
func (c *Client) request(ctx context.Context, m *pbui.Msg) (*pbui.Msg, error) {
	seq := atomic.AddUint64(&c.seq, 1)
	m.Seq = seq
	ch := make(chan *pbui.Msg, 1)
	c.mu.Lock()
	c.pending[seq] = ch
	c.mu.Unlock()
	if err := c.codec.Encode(m); err != nil {
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		return nil, err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			if e, _ := c.readErr.Load().(error); e != nil {
				return nil, e
			}
			return nil, fmt.Errorf("pbui: connection closed")
		}
		if r.T == pbui.TError {
			return r, fmt.Errorf("pbui: %s: %s", r.Code, r.Msg)
		}
		return r, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// RegisterVerbs contributes verbs to the broker's action table.
func (c *Client) RegisterVerbs(ctx context.Context, verbs []pbui.Verb) error {
	_, err := c.request(ctx, &pbui.Msg{T: pbui.TRegister, Verbs: verbs})
	return err
}

// Accept starts an accept session and blocks until an object is answered,
// the session is cancelled (returns nil, nil), or ctx expires. Mirrors
// ui.accept(type, prompt) in the prototype.
func (c *Client) Accept(ctx context.Context, ptypes []string, prompt string) (*pbui.Object, error) {
	r, err := c.request(ctx, &pbui.Msg{T: pbui.TAcceptStart, Ptypes: ptypes, Prompt: prompt})
	if err != nil {
		return nil, err
	}
	if r.T != pbui.TAcceptResult {
		return nil, fmt.Errorf("pbui: unexpected reply %q to accept.start", r.T)
	}
	return r.Object, nil // nil object = cancelled
}

// Answer resolves the pending accept session with obj. Empty session
// answers whatever session is pending.
func (c *Client) Answer(ctx context.Context, session string, obj pbui.Object) error {
	_, err := c.request(ctx, &pbui.Msg{T: pbui.TAcceptAnswer, Session: session, Object: &obj})
	return err
}

// Cancel cancels the pending accept session (Escape).
func (c *Client) Cancel(ctx context.Context, session string) error {
	_, err := c.request(ctx, &pbui.Msg{T: pbui.TAcceptCancel, Session: session})
	return err
}

// InvokeVerb asks the broker to route a verb to its owner.
func (c *Client) InvokeVerb(ctx context.Context, verbID string, obj pbui.Object) error {
	_, err := c.request(ctx, &pbui.Msg{T: pbui.TVerbInvoke, VerbID: verbID, Object: &obj})
	return err
}

// RequestMenu asks the WM (via the broker) to pop the verb menu for obj at
// screen position (x, y). If no WM is connected the broker returns the verb
// list instead, which is also returned here.
func (c *Client) RequestMenu(ctx context.Context, obj pbui.Object, x, y int) ([]pbui.Verb, error) {
	r, err := c.request(ctx, &pbui.Msg{T: pbui.TMenuRequest, Object: &obj, X: x, Y: y})
	if err != nil {
		return nil, err
	}
	if r.T == pbui.TVerbList {
		return r.Verbs, nil
	}
	return nil, nil
}

// QueryVerbs lists registered verbs, optionally filtered by ptype.
func (c *Client) QueryVerbs(ctx context.Context, ptype string) ([]pbui.Verb, error) {
	m := &pbui.Msg{T: pbui.TQueryVerbs}
	if ptype != "" {
		o, _ := pbui.NewObject(ptype, "")
		m.Object = &o
	}
	r, err := c.request(ctx, m)
	if err != nil {
		return nil, err
	}
	return r.Verbs, nil
}

// Hover feeds the mouse-doc line.
func (c *Client) Hover(text string) error {
	return c.codec.Encode(&pbui.Msg{T: pbui.TDocHover, Text: text})
}

// Emit adds an event to the bus (the trace).
func (c *Client) Emit(ctx context.Context, event string, data interface{}) error {
	raw, err := jsonMarshal(data)
	if err != nil {
		return err
	}
	_, err = c.request(ctx, &pbui.Msg{T: pbui.TEventEmit, Event: event, Data: raw})
	return err
}

// Events subscribes to the event bus and returns the delivery channel
// (closed when the connection dies).
func (c *Client) Events(ctx context.Context) (<-chan *pbui.Msg, error) {
	if _, err := c.request(ctx, &pbui.Msg{T: pbui.TSubscribe}); err != nil {
		return nil, err
	}
	return c.events, nil
}

// Name returns the client name announced in the hello handshake — the
// broker's ownership key for verbs and (GGWM-008 A2) script commands.
func (c *Client) Name() string { return c.name }
