package naxbuilder

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
)

// MaxFrameBytes bounds a single JSON frame (request or response).
const MaxFrameBytes = 64 * 1024 * 1024 // 64 MiB

var errFrameTooLarge = errors.New("frame exceeds max size")

// WriteFrame writes a single length-prefixed JSON frame to w: 4-byte big-endian
// length prefix followed by the UTF-8 JSON body.
func WriteFrame(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(body)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// ReadFrame reads one length-prefixed JSON frame into v. It rejects frames whose
// declared length exceeds maxBytes as well as short (truncated) reads.
func ReadFrame(r io.Reader, v any, maxBytes int) error {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(lenBuf[:])
	if int(n) > maxBytes {
		return errFrameTooLarge
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

// ServeConn accepts exactly one request/response exchange on conn: it reads a
// *NaxBuildRequest, hands it to handler, then writes either the *NaxBuildResponse
// or a *NaxBuildError frame, and closes. A handler error is surfaced on the wire
// as a *NaxBuildError and ServeConn returns nil — the exchange completed, only the
// outcome was an error. Only transport-level failures (bad frame, write error)
// are returned to the caller.
func ServeConn(conn net.Conn, handler func(*NaxBuildRequest) (*NaxBuildResponse, error)) error {
	defer conn.Close()

	var req NaxBuildRequest
	if err := ReadFrame(conn, &req, MaxFrameBytes); err != nil {
		// Best effort: tell the peer what went wrong, then abort.
		_ = WriteFrame(conn, &NaxBuildError{Error: err.Error()})
		return err
	}

	resp, err := handler(&req)
	if err != nil {
		if werr := WriteFrame(conn, &NaxBuildError{Error: err.Error()}); werr != nil {
			return werr
		}
		return nil // error carried on the wire; exchange is complete
	}
	return WriteFrame(conn, resp)
}
