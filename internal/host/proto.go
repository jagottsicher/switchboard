package host

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/jagottsicher/switchboard/internal/answer"
	"github.com/jagottsicher/switchboard/internal/screen"
	"github.com/jagottsicher/switchboard/internal/sessions"
)

// ProtocolVersion changes whenever requests or responses change incompatibly. Client and server
// must speak the same version.
const ProtocolVersion = 1

// maxLine is the longest line accepted in either direction. A hardcopy with scrollback can be
// large.
const maxLine = 64 << 20

// hello is the first line the server writes.
type hello struct {
	Switchboard string `json:"switchboard"` // always "switchboard"
	Protocol    int    `json:"protocol"`
	Version     string `json:"version"`
}

// Operations.
const (
	opList       = "list"
	opFind       = "find"
	opHardcopy   = "hardcopy"
	opStuff      = "stuff"
	opReadDialog = "read_dialog"
	opSendChoice = "send_choice"
)

type request struct {
	ID          int    `json:"id"`
	Op          string `json:"op"`
	Query       string `json:"query,omitempty"`
	Session     string `json:"session,omitempty"`
	Window      int    `json:"window,omitempty"`
	Scrollback  bool   `json:"scrollback,omitempty"`
	Text        string `json:"text,omitempty"`
	Enter       bool   `json:"enter,omitempty"`
	N           int    `json:"n,omitempty"`
	Label       string `json:"label,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type response struct {
	ID      int             `json:"id"`
	Error   string          `json:"error,omitempty"`
	Code    string          `json:"code,omitempty"`
	Infos   []sessions.Info `json:"infos,omitempty"`
	Warn    string          `json:"warn,omitempty"`
	Session *screen.Session `json:"session,omitempty"`
	Window  int             `json:"window,omitempty"`
	Lines   []string        `json:"lines,omitempty"`
	Dialog  *answer.Dialog  `json:"dialog,omitempty"`
}

// Error codes for errors that the client must be able to tell apart.
var codes = map[string]error{
	"no_dialog":  answer.ErrNoDialog,
	"no_choices": answer.ErrNoChoices,
	"changed":    answer.ErrChanged,
}

func errorCode(err error) string {
	for code, e := range codes {
		if errors.Is(err, e) {
			return code
		}
	}
	return ""
}

// remoteError is an error reported by the server. It wraps the known error of its code, so
// that errors.Is works on the client as it does locally.
type remoteError struct {
	msg  string
	code error
}

func (e remoteError) Error() string { return e.msg }
func (e remoteError) Unwrap() error { return e.code }

// Serve answers requests from in on out with the sessions of h until in ends. version is the
// version of this binary, reported in the hello line.
func Serve(in io.Reader, out io.Writer, h Host, version string) error {
	w := bufio.NewWriter(out)
	enc := json.NewEncoder(w)
	if err := enc.Encode(hello{Switchboard: "switchboard", Protocol: ProtocolVersion, Version: version}); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	for sc.Scan() {
		var req request
		var resp response
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			resp.Error = fmt.Sprintf("invalid request: %v", err)
		} else {
			resp = handle(h, req)
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
	return sc.Err()
}

func handle(h Host, req request) response {
	resp := response{ID: req.ID}
	var err error
	switch req.Op {
	case opList:
		var warn error
		resp.Infos, warn, err = h.List()
		if warn != nil {
			resp.Warn = warn.Error()
		}
	case opFind:
		var s screen.Session
		s, resp.Window, err = h.Find(req.Query)
		if err == nil {
			resp.Session = &s
		}
	case opHardcopy:
		resp.Lines, err = h.Hardcopy(req.Session, req.Window, req.Scrollback)
	case opStuff:
		err = h.Stuff(req.Session, req.Window, req.Text, req.Enter)
	case opReadDialog:
		var d answer.Dialog
		d, err = h.ReadDialog(req.Session, req.Window)
		if err == nil {
			resp.Dialog = &d
		}
	case opSendChoice:
		err = h.SendChoice(req.Session, req.Window, req.N, req.Label, req.Fingerprint)
	default:
		err = fmt.Errorf("unknown operation %q", req.Op)
	}
	if err != nil {
		resp.Error, resp.Code = err.Error(), errorCode(err)
	}
	return resp
}
