// Package lsp provides a minimal Language Server Protocol client (Content-Length
// framed JSON-RPC over a subprocess's stdio), so definitions/references can be
// resolved semantically instead of by grep.
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// WriteMessage encodes v as an LSP message: a Content-Length header block
// followed by the JSON body.
func WriteMessage(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// ReadMessage reads one Content-Length-framed message body from r.
func ReadMessage(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, err := strconv.Atoi(strings.TrimSpace(line[len("content-length:"):]))
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length: %w", err)
			}
			length = n
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}
