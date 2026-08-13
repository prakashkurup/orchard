// Package codeburn reads CodeBurn's client-facing menubar JSON through its
// resident stdio server. CodeBurn remains responsible for discovering agent
// sessions, normalizing token usage, pricing models, and classifying activity;
// orchard only consumes and renders the resulting local data.
package codeburn

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrUnavailable means CodeBurn is disabled or its executable is not on PATH.
var ErrUnavailable = errors.New("codeburn unavailable")

// Period is one of CodeBurn's supported dashboard windows.
type Period string

const (
	PeriodToday     Period = "today"
	PeriodWeek      Period = "week"
	PeriodThirtyDay Period = "30days"
	PeriodMonth     Period = "month"
	PeriodSixMonths Period = "all"
	PeriodLifetime  Period = "lifetime"
)

// Label returns the compact user-facing name for a period.
func (p Period) Label() string {
	switch p {
	case PeriodWeek:
		return "7 Days"
	case PeriodThirtyDay:
		return "30 Days"
	case PeriodMonth:
		return "This Month"
	case PeriodSixMonths:
		return "6 Months"
	case PeriodLifetime:
		return "Lifetime"
	default:
		return "Today"
	}
}

func validPeriod(p Period) bool {
	switch p {
	case PeriodToday, PeriodWeek, PeriodThirtyDay, PeriodMonth, PeriodSixMonths, PeriodLifetime:
		return true
	default:
		return false
	}
}

// Payload is the stable subset of `codeburn status --format menubar-json`
// rendered by orchard. Unknown fields are deliberately ignored so additive
// CodeBurn releases remain compatible.
type Payload struct {
	Generated string   `json:"generated"`
	Current   Current  `json:"current"`
	History   History  `json:"history"`
	Currency  Currency `json:"currency"`
}

type Current struct {
	Label            string             `json:"label"`
	Cost             float64            `json:"cost"`
	EstimatedCostUSD float64            `json:"estimatedCostUSD"`
	Calls            int                `json:"calls"`
	Sessions         int                `json:"sessions"`
	InputTokens      int                `json:"inputTokens"`
	OutputTokens     int                `json:"outputTokens"`
	CacheReadTokens  int                `json:"cacheReadTokens"`
	CacheWriteTokens int                `json:"cacheWriteTokens"`
	CacheHitPercent  float64            `json:"cacheHitPercent"`
	OneShotRate      *float64           `json:"oneShotRate"`
	PricingCoverage  *float64           `json:"pricingCoverage"`
	TopActivities    []Activity         `json:"topActivities"`
	TopModels        []Model            `json:"topModels"`
	TopProjects      []Project          `json:"topProjects"`
	ProviderDetails  []Provider         `json:"providerDetails"`
	Providers        map[string]float64 `json:"providers"`
	Tools            []CountRow         `json:"tools"`
	MCPServers       []CountRow         `json:"mcpServers"`
	Skills           []CostRow          `json:"skills"`
	Subagents        []CostRow          `json:"subagents"`
	UnpricedModels   []UnpricedModel    `json:"unpricedModels"`
	Workflow         Workflow           `json:"workflow"`
}

type Activity struct {
	Name        string   `json:"name"`
	Cost        float64  `json:"cost"`
	Turns       int      `json:"turns"`
	OneShotRate *float64 `json:"oneShotRate"`
}

type Model struct {
	Name             string  `json:"name"`
	Cost             float64 `json:"cost"`
	Calls            int     `json:"calls"`
	EstimatedCostUSD float64 `json:"estimatedCostUSD"`
}

type Project struct {
	Name              string  `json:"name"`
	Cost              float64 `json:"cost"`
	Sessions          int     `json:"sessions"`
	AvgCostPerSession float64 `json:"avgCostPerSession"`
}

type Provider struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Cost  float64 `json:"cost"`
}

type CountRow struct {
	Name  string `json:"name"`
	Calls int    `json:"calls"`
}

type CostRow struct {
	Name  string  `json:"name"`
	Calls int     `json:"calls"`
	Turns int     `json:"turns"`
	Cost  float64 `json:"cost"`
}

type UnpricedModel struct {
	Model  string `json:"model"`
	Calls  int    `json:"calls"`
	Tokens int    `json:"tokens"`
}

type Workflow struct {
	Corrections             int      `json:"corrections"`
	CorrectionRate          *float64 `json:"correctionRate"`
	MedianTimeToFirstEditMS *float64 `json:"medianTimeToFirstEditMs"`
}

type History struct {
	Daily []Daily `json:"daily"`
}

type Daily struct {
	Date             string  `json:"date"`
	Cost             float64 `json:"cost"`
	Calls            int     `json:"calls"`
	InputTokens      int     `json:"inputTokens"`
	OutputTokens     int     `json:"outputTokens"`
	CacheReadTokens  int     `json:"cacheReadTokens"`
	CacheWriteTokens int     `json:"cacheWriteTokens"`
}

type Currency struct {
	Code   string  `json:"code"`
	Symbol string  `json:"symbol"`
	Rate   float64 `json:"rate"`
}

// Client owns one warm `codeburn serve --stdio` child. Queries are serialized
// because CodeBurn's resident protocol and aggregation pipeline are serialized.
type Client struct {
	mu     sync.Mutex
	path   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	scan   *bufio.Scanner
	nextID uint64
	closed bool
}

// NewClient resolves CodeBurn from ORCHARD_CODEBURN or PATH. Set the variable
// to off/0/false to disable integration, or to an executable path to override.
func NewClient() *Client {
	setting := strings.TrimSpace(os.Getenv("ORCHARD_CODEBURN"))
	switch strings.ToLower(setting) {
	case "0", "false", "no", "off", "disabled":
		return &Client{}
	}
	if setting != "" && !strings.EqualFold(setting, "auto") {
		return NewClientWithExecutable(setting)
	}
	path, _ := exec.LookPath("codeburn")
	return NewClientWithExecutable(path)
}

// NewClientWithExecutable builds a client for an explicit CodeBurn executable.
func NewClientWithExecutable(path string) *Client {
	return &Client{path: strings.TrimSpace(path)}
}

// Available reports whether the client resolved an executable.
func (c *Client) Available() bool { return c != nil && c.path != "" }

// Query returns a project-root-scoped CodeBurn payload. The resident server is
// preferred; a one-shot status command is the compatibility fallback.
func (c *Client) Query(ctx context.Context, period Period, root string) (Payload, error) {
	if c == nil || c.path == "" {
		return Payload{}, fmt.Errorf("%w: install codeburn or set ORCHARD_CODEBURN", ErrUnavailable)
	}
	if !validPeriod(period) {
		return Payload{}, fmt.Errorf("invalid codeburn period %q", period)
	}
	args := queryArgs(period, root)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return Payload{}, errors.New("codeburn client is closed")
	}

	payload, err := c.queryResidentLocked(ctx, args)
	if err == nil {
		return payload, nil
	}
	c.stopLocked()
	return c.queryOnceLocked(ctx, args)
}

func queryArgs(period Period, root string) []string {
	args := []string{
		"status", "--format", "menubar-json",
		"--period", string(period),
		"--provider", "all",
		"--no-optimize", "--no-timeline",
	}
	root = normalizeRoot(root)
	if root != "" {
		args = append(args, "--project", root)
	}
	return args
}

func normalizeRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	if strings.HasPrefix(root, "~"+string(filepath.Separator)) {
		if home, err := os.UserHomeDir(); err == nil {
			root = filepath.Join(home, strings.TrimPrefix(root, "~"+string(filepath.Separator)))
		}
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return filepath.Clean(root)
}

type serveRequest struct {
	ID   uint64   `json:"id"`
	Args []string `json:"args"`
}

type serveResponse struct {
	ID      uint64 `json:"id"`
	OK      bool   `json:"ok"`
	Refused bool   `json:"refused"`
	Output  string `json:"output"`
	Error   string `json:"error"`
}

func (c *Client) queryResidentLocked(ctx context.Context, args []string) (Payload, error) {
	if err := c.ensureResidentLocked(ctx); err != nil {
		return Payload{}, err
	}
	c.nextID++
	id := c.nextID
	if err := json.NewEncoder(c.stdin).Encode(serveRequest{ID: id, Args: args}); err != nil {
		return Payload{}, fmt.Errorf("write codeburn request: %w", err)
	}
	line, err := c.readLineLocked(ctx)
	if err != nil {
		return Payload{}, err
	}
	var response serveResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return Payload{}, fmt.Errorf("decode codeburn response: %w", err)
	}
	if response.ID != id {
		return Payload{}, fmt.Errorf("codeburn response id %d, want %d", response.ID, id)
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "request failed"
		}
		return Payload{}, errors.New(response.Error)
	}
	return decodePayload([]byte(response.Output))
}

func (c *Client) ensureResidentLocked(ctx context.Context) error {
	if c.cmd != nil {
		return nil
	}
	cmd := exec.Command(c.path, "serve", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("start codeburn: %w", err)
	}
	c.cmd, c.stdin = cmd, stdin
	c.scan = bufio.NewScanner(stdout)
	c.scan.Buffer(make([]byte, 64*1024), 16*1024*1024)

	line, err := c.readLineLocked(ctx)
	if err != nil {
		c.stopLocked()
		return err
	}
	var ready struct {
		Ready bool `json:"ready"`
	}
	if json.Unmarshal(line, &ready) != nil || !ready.Ready {
		c.stopLocked()
		return errors.New("codeburn serve did not become ready")
	}
	return nil
}

func (c *Client) readLineLocked(ctx context.Context) ([]byte, error) {
	type result struct {
		line []byte
		err  error
	}
	ch := make(chan result, 1)
	scanner := c.scan
	go func() {
		if scanner != nil && scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			ch <- result{line: line}
			return
		}
		if scanner != nil && scanner.Err() != nil {
			ch <- result{err: scanner.Err()}
			return
		}
		ch <- result{err: io.EOF}
	}()
	select {
	case <-ctx.Done():
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("read codeburn response: %w", r.err)
		}
		return r.line, nil
	}
}

func (c *Client) queryOnceLocked(ctx context.Context, args []string) (Payload, error) {
	cmd := exec.CommandContext(ctx, c.path, args...)
	stdout, err := cmd.Output()
	if err != nil {
		return Payload{}, fmt.Errorf("codeburn status: %w", err)
	}
	return decodePayload(stdout)
}

func decodePayload(data []byte) (Payload, error) {
	var payload Payload
	if err := json.Unmarshal(data, &payload); err != nil {
		return Payload{}, fmt.Errorf("decode codeburn payload: %w", err)
	}
	if strings.TrimSpace(payload.Current.Label) == "" {
		return Payload{}, errors.New("incompatible codeburn payload: missing current.label")
	}
	if payload.Current.Cost < 0 || payload.Current.Calls < 0 || payload.Current.Sessions < 0 {
		return Payload{}, errors.New("incompatible codeburn payload: negative totals")
	}
	return payload, nil
}

// Close stops the resident child. Closing stdin is CodeBurn's documented clean
// shutdown signal; a short kill fallback prevents orchard exit from hanging.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return c.stopLocked()
}

func (c *Client) stopLocked() error {
	if c.cmd == nil {
		c.stdin, c.scan = nil, nil
		return nil
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	done := make(chan error, 1)
	cmd := c.cmd
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-time.After(300 * time.Millisecond):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		err = <-done
	}
	c.cmd, c.stdin, c.scan = nil, nil, nil
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ProcessState != nil {
		return nil // killed or closed children are expected during fallback/shutdown
	}
	return err
}
