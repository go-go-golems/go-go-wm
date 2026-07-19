package pbui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// Protocol is the wire protocol version announced in hello.
const Protocol = 1

// MaxFrameBytes bounds a single NDJSON frame; the decoder rejects longer
// lines (the broker eats untrusted bytes from arbitrary clients).
const MaxFrameBytes = 1 << 20

// Msg is one protocol frame. A single struct with optional fields keeps the
// codec trivial and the protocol socat-able; T discriminates.
type Msg struct {
	T   string `json:"t"`
	Seq uint64 `json:"seq,omitempty"` // client-scoped request id, echoed in replies

	// hello
	Name     string   `json:"name,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	Protocol int      `json:"protocol,omitempty"`

	// register / menu.show
	Verbs []Verb `json:"verbs,omitempty"`

	// accept.*
	Session string   `json:"session,omitempty"`
	Ptypes  []string `json:"ptypes,omitempty"`
	Prompt  string   `json:"prompt,omitempty"`
	Reason  string   `json:"reason,omitempty"`

	// object payloads (accept.answer/result, verb.invoke/run, menu.*)
	Object *Object `json:"object,omitempty"`

	// verb.invoke / verb.run
	VerbID string `json:"verb_id,omitempty"`

	// menu.request / menu.show (screen position hint)
	X int `json:"x,omitempty"`
	Y int `json:"y,omitempty"`

	// doc.hover
	Text string `json:"text,omitempty"`

	// event.emit / event
	Event    string          `json:"event,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	EventSeq uint64          `json:"event_seq,omitempty"`
	Source   string          `json:"source,omitempty"`

	// error
	Code string `json:"code,omitempty"`
	Msg  string `json:"msg,omitempty"`
}

// Message types.
const (
	// client → broker
	THello        = "hello"
	TRegister     = "register"
	TAcceptStart  = "accept.start"
	TAcceptAnswer = "accept.answer"
	TAcceptCancel = "accept.cancel"
	TVerbInvoke   = "verb.invoke"
	TMenuRequest  = "menu.request"
	TDocHover     = "doc.hover"
	TEventEmit    = "event.emit"
	TSubscribe    = "subscribe"
	TQueryVerbs   = "query.verbs"

	// broker → client
	TWelcome      = "welcome"
	TAcceptMode   = "accept.mode"
	TAcceptClear  = "accept.clear"
	TAcceptResult = "accept.result"
	TVerbRun      = "verb.run"
	TMenuShow     = "menu.show"
	TEvent        = "event"
	TVerbList     = "verb.list"
	TOK           = "ok"
	TError        = "error"
)

// Codec frames Msgs on a stream. NDJSON now; the interface is the seam for
// a deterministic-CBOR replacement later (design decision D4).
type Codec interface {
	Encode(m *Msg) error
	Decode() (*Msg, error)
}

// NDJSONCodec is newline-delimited JSON over an io.ReadWriter.
type NDJSONCodec struct {
	r *bufio.Reader
	w io.Writer
}

func NewNDJSONCodec(rw io.ReadWriter) *NDJSONCodec {
	return &NDJSONCodec{r: bufio.NewReaderSize(rw, 64<<10), w: rw}
}

func (c *NDJSONCodec) Encode(m *Msg) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = c.w.Write(b)
	return err
}

func (c *NDJSONCodec) Decode() (*Msg, error) {
	line, err := readBoundedLine(c.r, MaxFrameBytes)
	if err != nil {
		return nil, err
	}
	return DecodeFrame(line)
}

// DecodeFrame parses one frame; it is the fuzz target.
func DecodeFrame(line []byte) (*Msg, error) {
	var m Msg
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, fmt.Errorf("pbui: bad frame: %w", err)
	}
	if m.T == "" {
		return nil, fmt.Errorf("pbui: frame missing t")
	}
	return &m, nil
}

func readBoundedLine(r *bufio.Reader, limit int) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		buf = append(buf, chunk...)
		if len(buf) > limit {
			return nil, fmt.Errorf("pbui: frame exceeds %d bytes", limit)
		}
		if err == nil {
			return buf[:len(buf)-1], nil
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF && len(buf) > 0 {
			return buf, io.ErrUnexpectedEOF
		}
		return nil, err
	}
}
