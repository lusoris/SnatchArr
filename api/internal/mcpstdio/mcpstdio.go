// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package mcpstdio implements the `snatcharr mcp stdio` CLI subcommand.
//
// It acts as a lightweight proxy between newline-delimited JSON-RPC messages on stdin/stdout
// and the local SnatchArr Streamable HTTP endpoint (/api/v1/mcp).
// Framing follows the Model Context Protocol stdio transport specification:
// https://modelcontextprotocol.io/specification/2025-06-18/basic/transports
package mcpstdio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Command is the CLI argument dispatched by main.go.
const Command = "mcp"

// Subcommand is the only valid argument after Command.
const Subcommand = "stdio"

// maxLineBytes bounds the length of a single incoming JSON-RPC line on stdin (1 MiB).
const maxLineBytes = 1 << 20

// maxResponseBytes bounds the HTTP response body read from the server (4 MiB).
const maxResponseBytes = 4 << 20

// maxChunksPerLine bounds the number of read chunks per line to prevent infinite spinning.
const maxChunksPerLine = 4096

// requestTimeout bounds a single HTTP request to the MCP server.
const requestTimeout = 60 * time.Second

// JSON-RPC 2.0 error codes.
const (
	errCodeInvalidRequest = -32600
	errCodeInternal       = -32000
)

var (
	errStdoutWrite = errors.New("mcpstdio: stdout write failed")
	errLineTooLong = errors.New("mcpstdio: line exceeded max chunk iterations")
	idRegex        = regexp.MustCompile(`"id"\s*:\s*(-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?|"(?:\\.|[^"\\])*"|null)`)
)

type jsonRPCErrorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   jsonRPCError    `json:"error"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type errorData struct {
	Status          int    `json:"status"`
	WWWAuthenticate string `json:"www_authenticate,omitempty"`
}

type problemDetails struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Status int    `json:"status"`
}

type lineAccumulator struct {
	buf      bytes.Buffer
	overflow bool
}

func (a *lineAccumulator) appendChunk(chunk []byte) {
	if a.overflow {
		return
	}
	contentLen := a.buf.Len() + len(bytes.TrimRight(chunk, "\r\n"))
	if contentLen > maxLineBytes {
		a.overflow = true
		return
	}
	a.buf.Write(chunk)
}

func (a *lineAccumulator) handleEOF(chunk []byte) ([]byte, bool, error) {
	if len(chunk) > 0 {
		a.appendChunk(chunk)
	}
	if a.buf.Len() == 0 && !a.overflow {
		return nil, false, io.EOF
	}
	return a.buf.Bytes(), a.overflow, nil
}

// Run executes the stdio proxy with a default HTTP client.
func Run(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	return RunWithClient(ctx, &http.Client{Timeout: requestTimeout}, args, getenv, stdin, stdout, stderr)
}

// RunWithClient executes the stdio proxy using the provided HTTP client.
func RunWithClient(ctx context.Context, client *http.Client, args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	if !validateArgs(args, stderr) {
		return 2
	}
	endpoint, apiKey, ok := validateEnv(getenv, stderr)
	if !ok {
		return 2
	}
	return runLoop(ctx, client, endpoint, apiKey, stdin, stdout, stderr)
}

func runLoop(ctx context.Context, client *http.Client, endpoint, apiKey string, stdin io.Reader, stdout, stderr io.Writer) int {
	reader := bufio.NewReader(stdin)
	for ctx.Err() == nil {
		line, overflow, err := readLine(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0
			}
			report(stderr, fmt.Sprintf("snatcharr mcp: read stdin: %v", err))
			return 1
		}
		if perr := processLine(ctx, client, endpoint, apiKey, line, overflow, stdout, stderr); perr != nil {
			if errors.Is(perr, errStdoutWrite) {
				return 1
			}
		}
	}
	return 0
}

func processLine(ctx context.Context, client *http.Client, endpoint, apiKey string, line []byte, overflow bool, stdout, stderr io.Writer) error {
	if overflow {
		return handleOverflow(line, stdout, stderr)
	}
	trimmed := bytes.TrimRight(line, "\r\n")
	if len(bytes.TrimSpace(trimmed)) == 0 {
		return nil
	}
	return processMessage(ctx, client, endpoint, apiKey, trimmed, stdout, stderr)
}

func validateArgs(args []string, stderr io.Writer) bool {
	if len(args) != 1 || args[0] != Subcommand {
		report(stderr, "Usage: snatcharr mcp stdio")
		return false
	}
	return true
}

func validateEnv(getenv func(string) string, stderr io.Writer) (string, string, bool) {
	rawURL := strings.TrimSpace(getenv("SNATCHARR_URL"))
	if rawURL == "" {
		report(stderr, "snatcharr mcp: SNATCHARR_URL is required")
		return "", "", false
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		report(stderr, "snatcharr mcp: SNATCHARR_URL must be an http or https URL with host")
		return "", "", false
	}
	checkLoopbackWarning(u, stderr)
	apiKey := strings.TrimSpace(getenv("SNATCHARR_API_KEY"))
	if apiKey == "" {
		report(stderr, "snatcharr mcp: SNATCHARR_API_KEY is required")
		return "", "", false
	}
	endpoint := strings.TrimRight(rawURL, "/") + "/api/v1/mcp"
	return endpoint, apiKey, true
}

func checkLoopbackWarning(u *url.URL, stderr io.Writer) {
	if u.Scheme != "http" {
		return
	}
	host := u.Hostname()
	if host == "localhost" {
		return
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return
	}
	report(stderr, "snatcharr mcp: warning: SNATCHARR_URL uses unencrypted http with non-loopback host; api key travels in cleartext")
}

func readLine(r *bufio.Reader) ([]byte, bool, error) {
	var acc lineAccumulator
	for range maxChunksPerLine {
		chunk, err := r.ReadSlice('\n')
		if err == nil {
			acc.appendChunk(chunk)
			return acc.buf.Bytes(), acc.overflow, nil
		}
		if errors.Is(err, io.EOF) {
			return acc.handleEOF(chunk)
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, false, fmt.Errorf("read slice: %w", err)
		}
		acc.appendChunk(chunk)
	}
	return nil, false, errLineTooLong
}

func peekID(msg []byte) (json.RawMessage, bool) {
	var peek struct {
		ID *json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(msg, &peek); err == nil && peek.ID != nil {
		return *peek.ID, true
	}
	match := idRegex.FindSubmatch(msg)
	if len(match) > 1 {
		return json.RawMessage(match[1]), true
	}
	return nil, false
}

func handleOverflow(line []byte, stdout, stderr io.Writer) error {
	id, hasID := peekID(line)
	if hasID {
		return writeErrorResponse(stdout, id, errCodeInvalidRequest, "message exceeds 1 MiB", nil)
	}
	report(stderr, "snatcharr mcp: message exceeds 1 MiB")
	return nil
}

func processMessage(ctx context.Context, client *http.Client, endpoint, apiKey string, line []byte, stdout, stderr io.Writer) (err error) {
	id, hasID := peekID(line)
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(line))
	if err != nil {
		return handleDispatchError(err, apiKey, id, hasID, stdout, stderr)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return handleDispatchError(err, apiKey, id, hasID, stdout, stderr)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close response body: %w", cerr)
		}
	}()

	return handleResponse(resp, id, hasID, stdout, stderr)
}

func handleDispatchError(err error, apiKey string, id json.RawMessage, hasID bool, stdout, stderr io.Writer) error {
	errMsg := sanitizeKey(err.Error(), apiKey)
	if hasID {
		return writeErrorResponse(stdout, id, errCodeInternal, "snatcharr: "+errMsg, nil)
	}
	report(stderr, "snatcharr: "+errMsg)
	return nil
}

func sanitizeKey(msg, key string) string {
	if key != "" {
		return strings.ReplaceAll(msg, key, "[REDACTED]")
	}
	return msg
}

func handleResponse(resp *http.Response, id json.RawMessage, hasID bool, stdout, stderr io.Writer) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return handleReadError(err, id, hasID, stdout, stderr)
	}
	if resp.StatusCode == http.StatusOK {
		return handleOKResponse(resp, body, id, hasID, stdout, stderr)
	}
	if resp.StatusCode == http.StatusAccepted {
		return nil
	}
	return handleNon2xxResponse(resp, body, id, hasID, stdout, stderr)
}

func handleOKResponse(resp *http.Response, body []byte, id json.RawMessage, hasID bool, stdout, stderr io.Writer) error {
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		if hasID {
			return writeErrorResponse(stdout, id, errCodeInternal, "snatcharr: streaming text/event-stream response is unsupported", nil)
		}
		report(stderr, "snatcharr: streaming text/event-stream response is unsupported")
		return nil
	}

	trimmed := bytes.TrimSpace(body)
	var compact bytes.Buffer
	if len(trimmed) == 0 || json.Compact(&compact, trimmed) != nil {
		if hasID {
			return writeErrorResponse(stdout, id, errCodeInternal, "snatcharr: invalid JSON response from server", errorData{Status: http.StatusOK})
		}
		report(stderr, "snatcharr: invalid JSON response from server")
		return nil
	}

	compact.WriteByte('\n')
	if _, err := stdout.Write(compact.Bytes()); err != nil {
		return errStdoutWrite
	}
	return nil
}

func handleNon2xxResponse(resp *http.Response, body []byte, id json.RawMessage, hasID bool, stdout, stderr io.Writer) error {
	var prob problemDetails
	if err := json.Unmarshal(body, &prob); err != nil {
		prob.Title = http.StatusText(resp.StatusCode)
	}

	detail := prob.Detail
	if detail == "" {
		detail = prob.Title
	}
	if detail == "" {
		detail = http.StatusText(resp.StatusCode)
	}
	msg := fmt.Sprintf("snatcharr: HTTP %d: %s", resp.StatusCode, detail)

	if !hasID {
		report(stderr, msg)
		return nil
	}

	var data any
	if wwwAuth := resp.Header.Get("WWW-Authenticate"); wwwAuth != "" {
		data = errorData{
			Status:          resp.StatusCode,
			WWWAuthenticate: wwwAuth,
		}
	} else {
		data = errorData{
			Status: resp.StatusCode,
		}
	}
	return writeErrorResponse(stdout, id, errCodeInternal, msg, data)
}

func handleReadError(err error, id json.RawMessage, hasID bool, stdout, stderr io.Writer) error {
	msg := "snatcharr: read response body: " + err.Error()
	if hasID {
		return writeErrorResponse(stdout, id, errCodeInternal, msg, nil)
	}
	report(stderr, msg)
	return nil
}

func writeErrorResponse(w io.Writer, id json.RawMessage, code int, msg string, data any) error {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	resp := jsonRPCErrorResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: jsonRPCError{
			Code:    code,
			Message: msg,
			Data:    data,
		},
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal error response: %w", err)
	}
	payload = append(payload, '\n')
	if _, err := w.Write(payload); err != nil {
		return errStdoutWrite
	}
	return nil
}

func report(w io.Writer, msg string) {
	if _, werr := fmt.Fprintln(w, msg); werr != nil {
		return
	}
}
