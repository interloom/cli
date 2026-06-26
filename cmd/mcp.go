package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/interloom/cli/internal/client"
	"github.com/interloom/cli/internal/config"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

const (
	mcpCommandName     = "mcp"
	defaultMCPAddr     = "127.0.0.1:8765"
	defaultMCPEndpoint = "/mcp"
	argLimit           = "limit"
	toolFilesDownload  = "files_download"
	schemaKeyType      = "type"
	schemaKeyDesc      = "description"
)

var (
	flagMCPHTTP     bool
	flagMCPAddr     string
	flagMCPEndpoint string
)

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   mcpCommandName,
		Short: "Run an MCP server exposing Interloom tools",
		Long: "Run a Model Context Protocol server over stdio.\n\n" +
			"This default mode lets MCP clients launch `interloom mcp` directly. Pass\n" +
			"--http to instead serve Streamable HTTP on a loopback address. HTTP mode\n" +
			"has no MCP-level auth because it is restricted to localhost; API calls use\n" +
			"the same saved config token as the rest of the CLI.",
		Args: cobra.NoArgs,
		RunE: runMCP,
	}
	addConfigNameFlag(cmd)
	cmd.Flags().BoolVar(&flagMCPHTTP, "http", false,
		"serve MCP over local Streamable HTTP instead of stdio")
	cmd.Flags().StringVar(&flagMCPAddr, "addr", defaultMCPAddr,
		"loopback address for --http mode to listen on")
	cmd.Flags().StringVar(&flagMCPEndpoint, "endpoint", defaultMCPEndpoint,
		"HTTP endpoint path for --http mode")
	return cmd
}

func runMCP(cmd *cobra.Command, _ []string) error {
	endpoint, err := validateMCPModeFlags(cmd)
	if err != nil {
		return err
	}
	r, err := config.Resolve(flagConfigName, flagBaseURL)
	if err != nil {
		return err
	}

	mcpServer := newInterloomMCPServer(client.New(r.BaseURL, r.APIKey))
	if !flagMCPHTTP {
		return runMCPStdio(cmd, mcpServer)
	}
	return runMCPHTTP(cmd, mcpServer, endpoint, r.ConfigName)
}

func validateMCPModeFlags(cmd *cobra.Command) (string, error) {
	if !flagMCPHTTP {
		var httpOnly []string
		if cmd.Flags().Changed("addr") {
			httpOnly = append(httpOnly, "--addr")
		}
		if cmd.Flags().Changed("endpoint") {
			httpOnly = append(httpOnly, "--endpoint")
		}
		if len(httpOnly) > 0 {
			return "", fmt.Errorf("%s require --http", strings.Join(httpOnly, " and "))
		}
		return "", nil
	}
	if err := validateLoopbackAddr(flagMCPAddr); err != nil {
		return "", err
	}
	return normalizeMCPEndpoint(flagMCPEndpoint)
}

func runMCPStdio(cmd *cobra.Command, server *mcpsdk.Server) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return server.Run(ctx, &mcpsdk.StdioTransport{})
}

func runMCPHTTP(cmd *cobra.Command, mcpServer *mcpsdk.Server, endpoint, configName string) error {
	mux := http.NewServeMux()
	mux.Handle(endpoint, newMCPHTTPHandler(mcpServer))

	ln, err := net.Listen("tcp", flagMCPAddr)
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()

	httpServer := &http.Server{Handler: mux}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.Serve(ln)
	}()

	addr := listenerURLHost(ln.Addr())
	fmt.Fprintf(os.Stderr, "Interloom MCP server listening at http://%s%s using config %q. Press Ctrl-C to stop.\n", addr, endpoint, configName)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func validateLoopbackAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid --addr %q: %w", addr, err)
	}
	if port == "" {
		return fmt.Errorf("invalid --addr %q: missing port", addr)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("--addr must bind to localhost or a loopback IP, got %q", addr)
	}
	return nil
}

func normalizeMCPEndpoint(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("--endpoint cannot be empty")
	}
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	if strings.ContainsAny(endpoint, "?#") {
		return "", fmt.Errorf("--endpoint must be a path without query or fragment")
	}
	return endpoint, nil
}

func listenerURLHost(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return net.JoinHostPort(host, port)
}

func newMCPHTTPHandler(server *mcpsdk.Server) http.Handler {
	return mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, &mcpsdk.StreamableHTTPOptions{
		JSONResponse:   true,
		SessionTimeout: 30 * time.Minute,
	})
}

type mcpService struct {
	client *client.Client
}

func newInterloomMCPServer(c *client.Client) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "interloom",
		Title:   "Interloom CLI",
		Version: cliVersion(),
	}, &mcpsdk.ServerOptions{
		Instructions: "Use these tools to manage Interloom resources. Results are JSON returned from the Interloom API. The server uses the local CLI config for API authentication and never exposes the API token.",
	})
	svc := &mcpService{client: c}
	svc.registerResourceTools(server)
	svc.registerUserTools(server)
	svc.registerThreadTools(server)
	svc.registerFileTools(server)
	return server
}

func (s *mcpService) registerResourceTools(server *mcpsdk.Server) {
	for _, r := range apiResources() {
		r := r
		server.AddTool(&mcpsdk.Tool{
			Name:        r.name + "_list",
			Description: fmt.Sprintf("List %s", r.name),
			InputSchema: listInputSchema(r),
		}, s.listResourceHandler(r))
		server.AddTool(&mcpsdk.Tool{
			Name:        r.name + "_get",
			Description: fmt.Sprintf("Get a single %s by ID", r.singular),
			InputSchema: objectSchema(map[string]any{"id": stringSchema("resource ID")}, "id"),
		}, s.getResourceHandler(r))

		if r.readOnly {
			continue
		}
		if !r.noCreate {
			server.AddTool(&mcpsdk.Tool{
				Name:        r.name + "_create",
				Description: fmt.Sprintf("Create a %s", r.singular),
				InputSchema: bodyInputSchema(r, true),
			}, s.createResourceHandler(r))
		}
		server.AddTool(&mcpsdk.Tool{
			Name:        r.name + "_update",
			Description: fmt.Sprintf("Update a %s by ID", r.singular),
			InputSchema: updateInputSchema(r),
		}, s.updateResourceHandler(r))
		if !r.noDelete {
			server.AddTool(&mcpsdk.Tool{
				Name:        r.name + "_delete",
				Description: fmt.Sprintf("Delete a %s by ID", r.singular),
				InputSchema: objectSchema(map[string]any{"id": stringSchema("resource ID")}, "id"),
			}, s.deleteResourceHandler(r))
		}
	}
}

func (s *mcpService) registerUserTools(server *mcpsdk.Server) {
	server.AddTool(&mcpsdk.Tool{
		Name:        "users_me",
		Description: "Show the authenticated user",
		InputSchema: objectSchema(nil),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		if _, err := parseToolArgs(req); err != nil {
			return toolErrorResult(err), nil
		}
		raw, err := s.client.Get(ctx, "users", "me")
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolJSONResult(raw), nil
	})
}

func (s *mcpService) registerThreadTools(server *mcpsdk.Server) {
	server.AddTool(&mcpsdk.Tool{
		Name:        "threads_get",
		Description: "Get a single thread by ID",
		InputSchema: objectSchema(map[string]any{"id": stringSchema("thread ID")}, "id"),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args, err := parseToolArgs(req)
		if err != nil {
			return toolErrorResult(err), nil
		}
		id, err := args.requiredString("id")
		if err != nil {
			return toolErrorResult(err), nil
		}
		raw, err := s.client.Get(ctx, "threads", id)
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolJSONResult(raw), nil
	})

	server.AddTool(&mcpsdk.Tool{
		Name:        "threads_events",
		Description: "List a thread's events",
		InputSchema: objectSchema(map[string]any{
			"id":         stringSchema("thread ID"),
			argLimit:     integerSchema("maximum number of events to return"),
			keyCursor:    stringSchema("pagination cursor from a previous next_cursor"),
			keyDirection: stringSchema("sort direction: asc or desc"),
			"all":        boolSchema("fetch all pages and aggregate into a single list"),
		}, "id"),
	}, s.threadEventsHandler())

	server.AddTool(&mcpsdk.Tool{
		Name:        "threads_messages_create",
		Description: "Create a message in a thread",
		InputSchema: objectSchema(map[string]any{
			"thread_id": stringSchema("thread ID"),
			"text":      stringSchema("message text"),
			"data":      dataSchema("raw JSON request body"),
		}, "thread_id"),
	}, s.threadMessageCreateHandler())
}

func (s *mcpService) registerFileTools(server *mcpsdk.Server) {
	server.AddTool(&mcpsdk.Tool{
		Name:        "files_upload",
		Description: "Upload a file, optionally attached to a Space or Case",
		InputSchema: objectSchema(map[string]any{
			"path":     stringSchema("path to the local file to upload"),
			keySpaceID: stringSchema("attach the file to a Space"),
			keyCaseID:  stringSchema("attach the file to a Case"),
		}, "path"),
	}, s.fileUploadHandler())

	server.AddTool(&mcpsdk.Tool{
		Name:        toolFilesDownload,
		Description: "Download a file's bytes to a local output path",
		InputSchema: objectSchema(map[string]any{
			"id":  stringSchema("file ID"),
			"out": stringSchema("local path to write the downloaded bytes"),
		}, "id", "out"),
	}, s.fileDownloadHandler())
}

func (s *mcpService) listResourceHandler(r resource) mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args, err := parseToolArgs(req)
		if err != nil {
			return toolErrorResult(err), nil
		}
		q, err := listQueryFromArgs(args, r)
		if err != nil {
			return toolErrorResult(err), nil
		}
		all, err := args.bool("all")
		if err != nil {
			return toolErrorResult(err), nil
		}
		var raw json.RawMessage
		if all {
			raw, err = s.client.ListAll(ctx, r.name, q)
		} else {
			raw, err = s.client.List(ctx, r.name, q)
		}
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolJSONResult(raw), nil
	}
}

func (s *mcpService) getResourceHandler(r resource) mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args, err := parseToolArgs(req)
		if err != nil {
			return toolErrorResult(err), nil
		}
		id, err := args.requiredString("id")
		if err != nil {
			return toolErrorResult(err), nil
		}
		raw, err := s.client.Get(ctx, r.name, id)
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolJSONResult(raw), nil
	}
}

func (s *mcpService) createResourceHandler(r resource) mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args, err := parseToolArgs(req)
		if err != nil {
			return toolErrorResult(err), nil
		}
		body, err := bodyFromArgs(args, r, true)
		if err != nil {
			return toolErrorResult(err), nil
		}
		raw, err := s.client.Create(ctx, r.name, body)
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolJSONResult(raw), nil
	}
}

func (s *mcpService) updateResourceHandler(r resource) mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args, err := parseToolArgs(req)
		if err != nil {
			return toolErrorResult(err), nil
		}
		id, err := args.requiredString("id")
		if err != nil {
			return toolErrorResult(err), nil
		}
		body, err := bodyFromArgs(args, r, false)
		if err != nil {
			return toolErrorResult(err), nil
		}
		raw, err := s.client.Update(ctx, r.name, id, body)
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolJSONResult(raw), nil
	}
}

func (s *mcpService) deleteResourceHandler(r resource) mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args, err := parseToolArgs(req)
		if err != nil {
			return toolErrorResult(err), nil
		}
		id, err := args.requiredString("id")
		if err != nil {
			return toolErrorResult(err), nil
		}
		raw, err := s.client.Delete(ctx, r.name, id)
		if err != nil {
			return toolErrorResult(err), nil
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			raw = []byte(fmt.Sprintf(`{"id":%q,"deleted":true}`, id))
		}
		return toolJSONResult(raw), nil
	}
}

func (s *mcpService) threadEventsHandler() mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args, err := parseToolArgs(req)
		if err != nil {
			return toolErrorResult(err), nil
		}
		id, err := args.requiredString("id")
		if err != nil {
			return toolErrorResult(err), nil
		}
		q, err := threadEventsQueryFromArgs(args)
		if err != nil {
			return toolErrorResult(err), nil
		}
		resource := "threads/" + url.PathEscape(id) + "/events"
		all, err := args.bool("all")
		if err != nil {
			return toolErrorResult(err), nil
		}
		var raw json.RawMessage
		if all {
			raw, err = s.client.ListAll(ctx, resource, q)
		} else {
			raw, err = s.client.List(ctx, resource, q)
		}
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolJSONResult(raw), nil
	}
}

func (s *mcpService) threadMessageCreateHandler() mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args, err := parseToolArgs(req)
		if err != nil {
			return toolErrorResult(err), nil
		}
		threadID, err := args.requiredString("thread_id")
		if err != nil {
			return toolErrorResult(err), nil
		}
		text, hasText, err := args.string("text")
		if err != nil {
			return toolErrorResult(err), nil
		}
		data, hasData, err := args.object("data")
		if err != nil {
			return toolErrorResult(err), nil
		}
		if hasText && hasData {
			return toolErrorResult(fmt.Errorf("pass either text or data, not both")), nil
		}
		var body []byte
		switch {
		case hasData:
			body = data
		case hasText:
			body, err = json.Marshal(struct {
				Text string `json:"text"`
			}{Text: text})
			if err != nil {
				return toolErrorResult(err), nil
			}
		default:
			return toolErrorResult(fmt.Errorf("missing message body: pass text or data")), nil
		}
		resource := "threads/" + url.PathEscape(threadID) + "/messages"
		raw, err := s.client.Create(ctx, resource, body)
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolJSONResult(raw), nil
	}
}

func (s *mcpService) fileUploadHandler() mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args, err := parseToolArgs(req)
		if err != nil {
			return toolErrorResult(err), nil
		}
		path, err := args.requiredString("path")
		if err != nil {
			return toolErrorResult(err), nil
		}
		spaceID, _, err := args.string(keySpaceID)
		if err != nil {
			return toolErrorResult(err), nil
		}
		caseID, _, err := args.string(keyCaseID)
		if err != nil {
			return toolErrorResult(err), nil
		}
		raw, err := s.client.Upload(ctx, "files", path, map[string]string{
			keySpaceID: spaceID,
			keyCaseID:  caseID,
		})
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolJSONResult(raw), nil
	}
}

func (s *mcpService) fileDownloadHandler() mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args, err := parseToolArgs(req)
		if err != nil {
			return toolErrorResult(err), nil
		}
		id, err := args.requiredString("id")
		if err != nil {
			return toolErrorResult(err), nil
		}
		out, err := args.requiredString("out")
		if err != nil {
			return toolErrorResult(err), nil
		}
		raw, err := s.client.Get(ctx, "files", id)
		if err != nil {
			return toolErrorResult(err), nil
		}
		var f struct {
			DownloadURL string `json:"download_url"`
		}
		if err = json.Unmarshal(raw, &f); err != nil {
			return toolErrorResult(err), nil
		}
		if f.DownloadURL == "" {
			return toolErrorResult(fmt.Errorf("file %s has no download_url", id)), nil
		}
		dst, err := os.Create(out)
		if err != nil {
			return toolErrorResult(err), nil
		}
		defer func() { _ = dst.Close() }()
		if fetchErr := s.client.FetchTo(ctx, f.DownloadURL, dst); fetchErr != nil {
			return toolErrorResult(fetchErr), nil
		}
		payload, err := json.Marshal(struct {
			ID     string `json:"id"`
			Path   string `json:"path"`
			Status string `json:"status"`
		}{ID: id, Path: out, Status: "downloaded"})
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolJSONResult(payload), nil
	}
}

func listQueryFromArgs(args toolArgs, r resource) (url.Values, error) {
	q := url.Values{}
	if err := addIntQueryArg(q, args, argLimit); err != nil {
		return nil, err
	}
	if err := addStringQueryArg(q, args, keyCursor); err != nil {
		return nil, err
	}
	for _, f := range r.filters {
		if err := addFilterQueryArg(q, args, f); err != nil {
			return nil, err
		}
	}
	r.applyListDefaults(q)
	return q, nil
}

func threadEventsQueryFromArgs(args toolArgs) (url.Values, error) {
	q := url.Values{}
	for _, name := range []string{argLimit, keyCursor, keyDirection} {
		if name == argLimit {
			if err := addIntQueryArg(q, args, name); err != nil {
				return nil, err
			}
			continue
		}
		if err := addStringQueryArg(q, args, name); err != nil {
			return nil, err
		}
	}
	return q, nil
}

func addIntQueryArg(q url.Values, args toolArgs, name string) error {
	value, ok, err := args.int(name)
	if err != nil {
		return err
	}
	if ok {
		q.Set(name, fmt.Sprint(value))
	}
	return nil
}

func addStringQueryArg(q url.Values, args toolArgs, name string) error {
	value, ok, err := args.string(name)
	if err != nil {
		return err
	}
	if ok && value != "" {
		q.Set(name, value)
	}
	return nil
}

func addFilterQueryArg(q url.Values, args toolArgs, f filter) error {
	if !f.multi {
		return addStringQueryArg(q, args, f.name)
	}
	vals, ok, err := args.stringSlice(f.name)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	for _, v := range vals {
		if v != "" {
			q.Add(f.name, v)
		}
	}
	return nil
}

func bodyFromArgs(args toolArgs, r resource, create bool) ([]byte, error) {
	data, hasData, err := args.object("data")
	if err != nil {
		return nil, err
	}
	fields := r.fieldsFor(create)
	hasFields := hasAnyFieldArg(args, fields)
	if hasData && hasFields {
		return nil, fmt.Errorf("pass either data or typed fields, not both")
	}
	if hasData {
		return data, nil
	}
	if !hasFields {
		return nil, fmt.Errorf("no request body: pass data or one or more field arguments")
	}
	if create {
		if requiredErr := requireCreateFieldArgs(args, fields); requiredErr != nil {
			return nil, requiredErr
		}
	}
	out, err := bodyMapFromFieldArgs(args, fields)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func hasAnyFieldArg(args toolArgs, fields []field) bool {
	for _, f := range fields {
		if args.has(f.name) {
			return true
		}
	}
	return false
}

func requireCreateFieldArgs(args toolArgs, fields []field) error {
	var missing []string
	for _, f := range fields {
		if f.required && !args.has(f.name) {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required argument(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

func bodyMapFromFieldArgs(args toolArgs, fields []field) (map[string]any, error) {
	out := map[string]any{}
	for _, f := range fields {
		if !args.has(f.name) {
			continue
		}
		if f.multi {
			vals, _, err := args.stringSlice(f.name)
			if err != nil {
				return nil, err
			}
			out[f.name] = vals
			continue
		}
		v, _, err := args.string(f.name)
		if err != nil {
			return nil, err
		}
		out[f.name] = v
	}
	return out, nil
}

type toolArgs map[string]json.RawMessage

func parseToolArgs(req *mcpsdk.CallToolRequest) (toolArgs, error) {
	if req == nil || req.Params == nil || len(bytes.TrimSpace(req.Params.Arguments)) == 0 {
		return toolArgs{}, nil
	}
	var args toolArgs
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return nil, fmt.Errorf("arguments must be a JSON object: %w", err)
	}
	if args == nil {
		return toolArgs{}, nil
	}
	return args, nil
}

func (a toolArgs) has(name string) bool {
	raw, ok := a.raw(name)
	return ok && !isNull(raw)
}

func (a toolArgs) raw(name string) (json.RawMessage, bool) {
	raw, ok := a[name]
	if ok {
		return raw, true
	}
	if strings.Contains(name, "_") {
		raw, ok = a[strings.ReplaceAll(name, "_", "-")]
		if ok {
			return raw, true
		}
	}
	return nil, false
}

func (a toolArgs) requiredString(name string) (string, error) {
	v, ok, err := a.string(name)
	if err != nil {
		return "", err
	}
	if !ok || v == "" {
		return "", fmt.Errorf("missing required argument %q", name)
	}
	return v, nil
}

func (a toolArgs) string(name string) (string, bool, error) {
	raw, ok := a.raw(name)
	if !ok || isNull(raw) {
		return "", false, nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", true, fmt.Errorf("%s must be a string", name)
	}
	return v, true, nil
}

func (a toolArgs) int(name string) (int, bool, error) {
	raw, ok := a.raw(name)
	if !ok || isNull(raw) {
		return 0, false, nil
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, true, fmt.Errorf("%s must be an integer", name)
	}
	return v, true, nil
}

func (a toolArgs) bool(name string) (bool, error) {
	raw, ok := a.raw(name)
	if !ok || isNull(raw) {
		return false, nil
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return v, nil
}

func (a toolArgs) stringSlice(name string) ([]string, bool, error) {
	raw, ok := a.raw(name)
	if !ok || isNull(raw) {
		return nil, false, nil
	}
	var vals []string
	if err := json.Unmarshal(raw, &vals); err == nil {
		return vals, true, nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, true, fmt.Errorf("%s must be an array of strings", name)
	}
	return splitStringSlice(single), true, nil
}

func (a toolArgs) object(name string) ([]byte, bool, error) {
	raw, ok := a.raw(name)
	if !ok || isNull(raw) {
		return nil, false, nil
	}
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return nil, true, fmt.Errorf("%s must be a JSON object", name)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, true, fmt.Errorf("%s must be a JSON object: %w", name, err)
	}
	return raw, true, nil
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func splitStringSlice(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func toolJSONResult(raw json.RawMessage) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: jsonText(raw)}},
		StructuredContent: structuredContent(raw),
	}
}

func toolErrorResult(err error) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: toolErrorText(err)}},
		IsError: true,
	}
}

func toolErrorText(err error) string {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && len(bytes.TrimSpace(apiErr.Body)) > 0 && json.Valid(apiErr.Body) {
		return jsonText(apiErr.Body)
	}
	return err.Error()
}

func jsonText(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err == nil {
		return buf.String()
	}
	return string(raw)
}

func structuredContent(raw json.RawMessage) any {
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	return obj
}

func listInputSchema(r resource) map[string]any {
	props := map[string]any{
		argLimit:  integerSchema("maximum number of items to return"),
		keyCursor: stringSchema("pagination cursor from a previous next_cursor"),
		"all":     boolSchema("fetch all pages and aggregate into a single list"),
	}
	for _, f := range r.filters {
		if f.multi {
			props[f.name] = stringArraySchema(f.usage)
		} else {
			props[f.name] = stringSchema(f.usage)
		}
	}
	return objectSchema(props)
}

func bodyInputSchema(r resource, create bool) map[string]any {
	props := map[string]any{"data": dataSchema("raw JSON request body; use this instead of typed field arguments")}
	for _, f := range r.fieldsFor(create) {
		desc := f.usage
		if create && f.required {
			desc += " (required unless data is provided)"
		}
		if f.multi {
			props[f.name] = stringArraySchema(desc)
		} else {
			props[f.name] = stringSchema(desc)
		}
	}
	return objectSchema(props)
}

func updateInputSchema(r resource) map[string]any {
	schema := bodyInputSchema(r, false)
	props, _ := schema["properties"].(map[string]any)
	props["id"] = stringSchema("resource ID")
	schema["required"] = []string{"id"}
	return schema
}

func objectSchema(props map[string]any, required ...string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	schema := map[string]any{
		schemaKeyType: "object",
		"properties":  props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description string) map[string]any {
	return map[string]any{schemaKeyType: "string", schemaKeyDesc: description}
}

func integerSchema(description string) map[string]any {
	return map[string]any{schemaKeyType: "integer", schemaKeyDesc: description}
}

func boolSchema(description string) map[string]any {
	return map[string]any{schemaKeyType: "boolean", schemaKeyDesc: description}
}

func stringArraySchema(description string) map[string]any {
	return map[string]any{schemaKeyType: "array", "items": map[string]any{schemaKeyType: "string"}, schemaKeyDesc: description}
}

func dataSchema(description string) map[string]any {
	return map[string]any{schemaKeyType: "object", schemaKeyDesc: description, "additionalProperties": true}
}
