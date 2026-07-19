// Package pbui defines the PBUI object model and wire protocol: typed
// presentation values, verbs, accept-session messages, and their framing.
// It is the contract every other component depends on and contains no I/O.
package pbui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Object is a typed presentation value: the Go form of the prototype's
// <P ptype value> (pbui-shell.jsx:52-73). Ptype is an open string namespace
// ("color", "number", "file", "git-commit", "tile", "workspace", ...).
type Object struct {
	Ptype string          `json:"ptype"`
	Value json.RawMessage `json:"value"`
	Label string          `json:"label,omitempty"` // display face fallback
	Doc   string          `json:"doc,omitempty"`   // mouse-doc line text
}

// NewObject builds an Object from any JSON-representable value.
func NewObject(ptype string, value interface{}) (Object, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return Object{}, fmt.Errorf("pbui: value for %q not JSON-representable: %w", ptype, err)
	}
	return Object{Ptype: ptype, Value: raw}, nil
}

// StringValue returns the value as a plain string when it is one (or a
// number rendered as text); otherwise the raw JSON.
func (o Object) StringValue() string {
	var s string
	if err := json.Unmarshal(o.Value, &s); err == nil {
		return s
	}
	return string(o.Value)
}

// TypeMatches ports the prototype's typeMatches (pbui-shell.jsx:44-45):
// "any" matches everything, otherwise exact membership.
func TypeMatches(want []string, have string) bool {
	for _, w := range want {
		if w == "any" || w == have {
			return true
		}
	}
	return false
}

// Verb is an entry in the type-directed action table (the Go form of
// actionsFor, pbui-shell.jsx:712-760). Verbs are data; the broker routes
// verb.invoke to the client that registered the verb.
type Verb struct {
	ID      string   `json:"id"`                // "color.mix", "tile.split-right"
	Label   string   `json:"label"`             // "Mix with…  (accept a color)"
	Ptypes  []string `json:"ptypes"`            // ptypes it applies to; ["any"] allowed
	Accepts []string `json:"accepts,omitempty"` // ptypes the verb will accept() when run
	Owner   string   `json:"owner,omitempty"`   // filled by the broker: registering client
}

// --- pbui:// URIs ----------------------------------------------------------
//
// The URI form carries an Object through OSC 8 hyperlinks and open_actions:
//
//	pbui://<ptype>/<url-encoded-string-value>        scalar string values
//	pbui://<ptype>/?v=<base64url(json)>              anything else
//
// ObjectToURI / ObjectFromURI own this bijection; nothing else parses URIs.

const URIScheme = "pbui"

// ObjectToURI encodes an object as a pbui:// URI.
func ObjectToURI(o Object) string {
	var s string
	if err := json.Unmarshal(o.Value, &s); err == nil {
		return fmt.Sprintf("%s://%s/%s", URIScheme, url.PathEscape(o.Ptype), url.PathEscape(s))
	}
	enc := base64.RawURLEncoding.EncodeToString(o.Value)
	return fmt.Sprintf("%s://%s/?v=%s", URIScheme, url.PathEscape(o.Ptype), enc)
}

// ObjectFromURI decodes a pbui:// URI back into an Object.
func ObjectFromURI(uri string) (Object, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return Object{}, fmt.Errorf("pbui: bad uri: %w", err)
	}
	if u.Scheme != URIScheme {
		return Object{}, fmt.Errorf("pbui: not a %s:// uri: %q", URIScheme, uri)
	}
	ptype, err := url.PathUnescape(u.Host)
	if err != nil || ptype == "" {
		return Object{}, fmt.Errorf("pbui: bad ptype in uri %q", uri)
	}
	if v := u.Query().Get("v"); v != "" {
		raw, err := base64.RawURLEncoding.DecodeString(v)
		if err != nil {
			return Object{}, fmt.Errorf("pbui: bad structured value: %w", err)
		}
		if !json.Valid(raw) {
			return Object{}, fmt.Errorf("pbui: structured value is not valid JSON")
		}
		return Object{Ptype: ptype, Value: raw}, nil
	}
	s, err := url.PathUnescape(strings.TrimPrefix(u.Path, "/"))
	if err != nil {
		return Object{}, fmt.Errorf("pbui: bad value in uri %q", uri)
	}
	raw, _ := json.Marshal(s)
	return Object{Ptype: ptype, Value: raw}, nil
}
