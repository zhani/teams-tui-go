package main

// teams-for-linux backend.
//
// Instead of its own OAuth device-code sign-in, the TUI can use a running
// teams-for-linux app (terminalApi.enabled) as its backend: every request to
// graph.microsoft.com is sent over the app's local Unix socket, and the app
// performs it with the token of its signed-in Teams session. Chat reads that
// token cannot do are answered by the app from the Teams chat service in
// Graph shape (see app/terminalApi/graphProxy.js).
//
// It is used whenever the socket exists. TEAMS_TUI_BACKEND=direct restores
// the upstream device-code flow.

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"
)

const (
	backendSocketName = "teams-for-linux-terminal.sock"
	// Placeholder handed to code that expects a bearer token; the app adds
	// the real one, so no credential ever exists in this process.
	backendToken = "teams-for-linux-backend"
)

var backendRequestID atomic.Int64

// backendSocketPath mirrors app/terminalApi: $TEAMS_CLI_SOCKET, else
// $XDG_RUNTIME_DIR/teams-for-linux-terminal.sock.
func backendSocketPath() string {
	if p := os.Getenv("TEAMS_CLI_SOCKET"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config", "teams-for-linux")
	}
	return filepath.Join(dir, backendSocketName)
}

// teamsForLinuxBackend reports whether requests go through teams-for-linux.
func teamsForLinuxBackend() bool {
	if os.Getenv("TEAMS_TUI_BACKEND") == "direct" {
		return false
	}
	info, err := os.Stat(backendSocketPath())
	return err == nil && info.Mode()&os.ModeSocket != 0
}

type backendRequest struct {
	ID     int64              `json:"id"`
	Method string             `json:"method"`
	Params backendGraphParams `json:"params"`
}

type backendGraphParams struct {
	Method      string `json:"method"`
	URL         string `json:"url"`
	ContentType string `json:"contentType,omitempty"`
	Range       string `json:"range,omitempty"`
	Body        string `json:"body,omitempty"`
}

type backendResponse struct {
	ID     int64  `json:"id"`
	Error  string `json:"error,omitempty"`
	Result *struct {
		Status      int    `json:"status"`
		ContentType string `json:"contentType"`
		Body        string `json:"body"`
	} `json:"result,omitempty"`
}

// socketTransport sends graph.microsoft.com requests to teams-for-linux and
// everything else (e.g. pre-signed SharePoint download URLs) to next.
type socketTransport struct {
	path string
	next http.RoundTripper
}

func (t *socketTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "graph.microsoft.com" {
		return t.next.RoundTrip(req)
	}

	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	msg := backendRequest{
		ID:     backendRequestID.Add(1),
		Method: "graph",
		Params: backendGraphParams{
			Method:      req.Method,
			URL:         req.URL.String(),
			ContentType: req.Header.Get("Content-Type"),
			Range:       req.Header.Get("Range"),
		},
	}
	if len(body) > 0 {
		msg.Params.Body = base64.StdEncoding.EncodeToString(body)
	}
	line, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	// One connection per request keeps concurrent polling simple; the
	// socket is local, so this is cheap.
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(req.Context(), "unix", t.path)
	if err != nil {
		return nil, fmt.Errorf("teams-for-linux is not reachable at %s (is it running with terminalApi.enabled?): %w", t.path, err)
	}
	defer conn.Close()
	if deadline, ok := req.Context().Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(90 * time.Second))
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return nil, err
	}

	reader := bufio.NewReaderSize(conn, 1<<20)
	for {
		raw, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("teams-for-linux backend: %w", err)
		}
		var resp backendResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("teams-for-linux backend: bad response: %w", err)
		}
		if resp.ID != msg.ID {
			continue // events or stale replies
		}
		if resp.Error != "" {
			return nil, errors.New("teams-for-linux backend: " + resp.Error)
		}
		if resp.Result == nil {
			return nil, errors.New("teams-for-linux backend: empty response")
		}
		data, err := base64.StdEncoding.DecodeString(resp.Result.Body)
		if err != nil {
			return nil, fmt.Errorf("teams-for-linux backend: bad body: %w", err)
		}
		header := http.Header{}
		if resp.Result.ContentType != "" {
			header.Set("Content-Type", resp.Result.ContentType)
		}
		header.Set("Content-Length", strconv.Itoa(len(data)))
		return &http.Response{
			Status:        fmt.Sprintf("%d %s", resp.Result.Status, http.StatusText(resp.Result.Status)),
			StatusCode:    resp.Result.Status,
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        header,
			Body:          io.NopCloser(bytes.NewReader(data)),
			ContentLength: int64(len(data)),
			Request:       req,
		}, nil
	}
}

// installBackendTransport routes the HTTP clients used for Graph through
// teams-for-linux when the backend is active.
func installBackendTransport() {
	if !teamsForLinuxBackend() {
		return
	}
	transport := &socketTransport{path: backendSocketPath(), next: http.DefaultTransport}
	http.DefaultClient.Transport = transport
	downloadClient.Transport = transport
}
