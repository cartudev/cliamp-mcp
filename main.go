// Command cliamp-mcp is a local MCP server for a running cliamp player.
//
// It speaks the Model Context Protocol over stdio and forwards every call to
// cliamp's own IPC socket (~/.config/cliamp/cliamp.sock, mode 0600), so the
// player keeps owning playback, the queue and the providers. Nothing is
// exposed on the network: the socket only exists while cliamp runs and only
// the owning user can connect to it.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/bjarneo/cliamp/ipc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.2.2"

// client talks to one cliamp instance.
type client struct {
	socket string
}

// call sends one V2 request and returns the raw response as pretty JSON.
// A submitted job that is still running is polled until it finishes.
func (c client) call(ctx context.Context, request ipc.V2Request, deadline time.Duration) (string, error) {
	response, err := c.exchange(request, deadline)
	if err != nil {
		return "", err
	}
	if response.Job != nil && !terminal(response.Job.State) {
		response, err = c.waitJob(ctx, response.Job.ID)
		if err != nil {
			return "", err
		}
	}
	if response.Job != nil && (response.Job.State == ipc.JobFailed || response.Job.State == ipc.JobCanceled) {
		return "", jobError(response.Job)
	}
	return pretty(response)
}

// exchange sends one request and checks the envelope. It does not wait for jobs.
func (c client) exchange(request ipc.V2Request, deadline time.Duration) (ipc.V2Response, error) {
	conn, err := net.DialTimeout("unix", c.socket, 3*time.Second)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
			return ipc.V2Response{}, fmt.Errorf("cliamp is not running (no socket at %s)", c.socket)
		}
		return ipc.V2Response{}, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(deadline)); err != nil {
		return ipc.V2Response{}, fmt.Errorf("set deadline: %w", err)
	}
	data, err := json.Marshal(request)
	if err != nil {
		return ipc.V2Response{}, fmt.Errorf("marshal request: %w", err)
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return ipc.V2Response{}, fmt.Errorf("write: %w", err)
	}
	payload, err := readFrame(conn)
	if err != nil {
		return ipc.V2Response{}, fmt.Errorf("read response: %w", err)
	}
	if len(payload) == 0 {
		return ipc.V2Response{}, fmt.Errorf("no response from cliamp")
	}
	var response ipc.V2Response
	if err := json.Unmarshal(payload, &response); err != nil {
		return ipc.V2Response{}, fmt.Errorf("unmarshal response: %w", err)
	}
	if response.Version != 2 {
		return ipc.V2Response{}, fmt.Errorf("unexpected protocol version %d", response.Version)
	}
	if !bytes.Equal(bytes.TrimSpace(request.ID), bytes.TrimSpace(response.ID)) {
		return ipc.V2Response{}, fmt.Errorf("response ID does not match request")
	}
	if !response.OK {
		if response.Error != nil {
			return ipc.V2Response{}, fmt.Errorf("cliamp: %s: %s", response.Error.Code, response.Error.Message)
		}
		return ipc.V2Response{}, fmt.Errorf("cliamp: request failed")
	}
	return response, nil
}

// pretty renders v as indented JSON for the model to read. Cover art arrives
// as a data: URL and can be hundreds of kilobytes, so those strings are
// shortened before they reach the model.
func pretty(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode response: %w", err)
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	out, err := json.MarshalIndent(stripArt(decoded), "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode response: %w", err)
	}
	return string(out), nil
}

// id builds a request id from the tool name. Responses correlate by id.
func id(name string) json.RawMessage {
	data, _ := json.Marshal(name)
	return data
}

func main() {
	socket := os.Getenv("CLIAMP_SOCKET")
	if socket == "" {
		socket = ipc.DefaultSocketPath()
	}
	cli := client{socket: socket}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "cliamp",
		Version: version,
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "cliamp_state",
		Description: "Read the cliamp runtime snapshot: the current track, playback state, position, volume, modes, EQ and revisions.",
	}, cli.state)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "cliamp_capabilities",
		Description: "List the operations the running cliamp accepts, with their parameter names. Headless mode omits theme and vis.",
	}, cli.capabilities)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "cliamp_queue",
		Description: "List the loaded playlist, or filter it with query. Pages a few tracks and omits cover art. A query scans the playlist already loaded, which is how providers without search (Telegram) are filtered. It does not browse other playlists.",
	}, cli.queue)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "cliamp_call",
		Description: "Run any cliamp operation and wait for it to finish. Use cliamp_capabilities for the operation names and parameters. Examples: operation \"next\"; operation \"volume\" with params {\"value\": -6}; operation \"provider.search\" with params {\"provider\": \"spotify\", \"query\": \"daft punk\"}.",
	}, cli.operation)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "cliamp_providers",
		Description: "List the providers configured in the running cliamp.",
	}, cli.providers)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "cliamp_search",
		Description: "Search one provider. If that provider has no catalog search, fall back to filtering the playlist already loaded. Does not browse other playlists.",
	}, cli.search)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("cliamp-mcp: %v", err)
	}
}

// state reads the runtime snapshot.
func (c client) state(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	body, err := c.call(ctx, ipc.V2Request{ID: id("state"), Method: "state.get"}, 5*time.Second)
	return text(body, err)
}

// capabilities lists the operations the running instance accepts.
func (c client) capabilities(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	body, err := c.call(ctx, ipc.V2Request{ID: id("capabilities"), Method: "capabilities"}, 5*time.Second)
	return text(body, err)
}

// queueInput selects a slice of the loaded playlist, or filters it.
type queueInput struct {
	Offset *int   `json:"offset,omitempty" jsonschema:"zero-based start; omit to open a window around the current track"`
	Limit  int    `json:"limit,omitempty" jsonschema:"how many tracks to return, default 12, max 40"`
	Query  string `json:"query,omitempty" jsonschema:"filter the loaded playlist by artist, title or album"`
}

// queue lists or filters the playlist that is already loaded.
func (c client) queue(ctx context.Context, _ *mcp.CallToolRequest, in queueInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Query) != "" {
		found, err := c.findInPlaylist(ctx, in.Query, in.Limit)
		if err != nil {
			return text("", err)
		}
		body, err := pretty(found)
		return text(body, err)
	}
	offset := 0
	if in.Offset != nil {
		offset = *in.Offset
	} else if current, err := c.currentIndex(ctx); err == nil {
		offset = current - 2
		if offset < 0 {
			offset = 0
		}
	}
	live, err := c.window(ctx, "queue.list", offset, in.Limit)
	if err != nil {
		return text("", err)
	}
	next, err := c.window(ctx, "playnext.list", 0, defaultWindow)
	if err != nil {
		return text("", err)
	}
	body, err := pretty(map[string]any{"playlist": live, "play_next": next})
	return text(body, err)
}

// operationInput is the argument of cliamp_call.
type operationInput struct {
	Operation string         `json:"operation" jsonschema:"the operation name, for example next, pause, seek or provider.load"`
	Params    map[string]any `json:"params,omitempty" jsonschema:"operation parameters; omit when the operation takes none"`
}

// operation runs one cliamp operation and waits for the finished job.
func (c client) operation(ctx context.Context, _ *mcp.CallToolRequest, in operationInput) (*mcp.CallToolResult, any, error) {
	if in.Operation == "" {
		return text("", fmt.Errorf("operation is required; call cliamp_capabilities to list them"))
	}
	if listOperation(in.Operation) {
		offset, limit := 0, defaultWindow
		if n, ok := number(in.Params["offset"]); ok {
			offset = n
		}
		if n, ok := number(in.Params["limit"]); ok {
			limit = n
		}
		page, err := c.window(ctx, in.Operation, offset, limit)
		if err != nil {
			return text("", err)
		}
		body, err := pretty(page)
		return text(body, err)
	}
	params, err := json.Marshal(in.Params)
	if err != nil {
		return text("", fmt.Errorf("encode params: %w", err))
	}
	body, err := c.call(ctx, ipc.V2Request{
		ID:        id("call"),
		Method:    "operation.submit",
		Operation: in.Operation,
		Params:    params,
	}, 15*time.Second)
	return text(body, err)
}

// terminal reports whether a job will not change anymore.
func terminal(state ipc.JobState) bool {
	switch state {
	case ipc.JobSucceeded, ipc.JobFailed, ipc.JobCanceled:
		return true
	}
	return false
}

// jobError formats a finished job that did not succeed.
func jobError(job *ipc.Job) error {
	if job == nil {
		return fmt.Errorf("cliamp: job failed")
	}
	if job.Error != nil {
		if job.Error.Detail != "" {
			return fmt.Errorf("cliamp: %s: %s (%s)", job.Error.Code, job.Error.Message, job.Error.Detail)
		}
		return fmt.Errorf("cliamp: %s: %s", job.Error.Code, job.Error.Message)
	}
	return fmt.Errorf("cliamp: job %s", job.State)
}

// waitJob polls a job until it finishes or the context ends. job.get reads
// the id from the envelope field, not from params.
func (c client) waitJob(ctx context.Context, jobID string) (ipc.V2Response, error) {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if err := ctx.Err(); err != nil {
			return ipc.V2Response{}, err
		}
		if time.Now().After(deadline) {
			return ipc.V2Response{}, fmt.Errorf("cliamp: job %s still running after 2 minutes", jobID)
		}
		response, err := c.exchange(ipc.V2Request{
			ID:     id("job"),
			Method: "job.get",
			JobID:  jobID,
		}, 10*time.Second)
		if err != nil {
			return ipc.V2Response{}, err
		}
		if response.Job != nil && terminal(response.Job.State) {
			return response, nil
		}
		select {
		case <-ctx.Done():
			return ipc.V2Response{}, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// providers lists the configured providers.
func (c client) providers(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	body, err := c.call(ctx, ipc.V2Request{ID: id("providers"), Method: "operation.submit", Operation: "provider.list"}, 30*time.Second)
	return text(body, err)
}

// searchInput is the argument of cliamp_search.
type searchInput struct {
	Provider string `json:"provider" jsonschema:"provider key, for example spotify, telegram or local"`
	Query    string `json:"query" jsonschema:"text to search for"`
}

// search searches one provider.
func (c client) search(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, any, error) {
	if in.Provider == "" || in.Query == "" {
		return text("", fmt.Errorf("provider and query are required"))
	}
	params, err := json.Marshal(map[string]string{"provider": in.Provider, "query": in.Query})
	if err != nil {
		return text("", err)
	}
	body, err := c.call(ctx, ipc.V2Request{
		ID:        id("search"),
		Method:    "operation.submit",
		Operation: "provider.search",
		Params:    params,
	}, 60*time.Second)
	if unsupportedSearch(err) {
		found, scanErr := c.findInPlaylist(ctx, in.Query, maxHits)
		if scanErr != nil {
			return text("", scanErr)
		}
		found["provider"] = in.Provider
		found["note"] = "this provider has no catalog search; matched the playlist already loaded"
		scanned, prettyErr := pretty(found)
		return text(scanned, prettyErr)
	}
	return text(body, err)
}

// text wraps a tool result. A non-nil error becomes an MCP tool error so the
// model sees the message instead of a transport failure.
func text(body string, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: body}},
	}, nil, nil
}
