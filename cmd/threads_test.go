package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/interloom/cli/internal/client"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	testThreadID    = "thread-1"
	testMessageText = "hello"
	testFileID1     = "file-1"
	testFileID2     = "file-2"
)

func TestThreadsMessagesCreateCommandShape(t *testing.T) {
	threads := newThreadsCmd()
	if child, _, err := threads.Find([]string{"messages", commandUseCreate, testThreadID}); err != nil || child == nil || child.Use != "create <thread-id>" {
		t.Fatalf("messages create command not registered: child=%v err=%v", child, err)
	}
	for _, child := range threads.Commands() {
		if child.Name() == "message" {
			t.Fatalf("old singular message command is still registered")
		}
	}
}

func TestThreadMessageBodyFromText(t *testing.T) {
	cmd := newThreadsMessagesCreateCmd()
	if err := cmd.Flags().Set("text", testMessageText); err != nil {
		t.Fatalf("set text: %v", err)
	}
	body, err := threadMessageBody(cmd)
	if err != nil {
		t.Fatalf("threadMessageBody: %v", err)
	}
	if got, want := string(body), `{"text":"hello"}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestThreadMessageBodyFromTextAndFileIDs(t *testing.T) {
	cmd := newThreadsMessagesCreateCmd()
	if err := cmd.Flags().Set("text", testMessageText); err != nil {
		t.Fatalf("set text: %v", err)
	}
	if err := cmd.Flags().Set(threadMessageFileIDsFlag, testFileID1+","+testFileID2); err != nil {
		t.Fatalf("set file IDs: %v", err)
	}
	body, err := threadMessageBody(cmd)
	if err != nil {
		t.Fatalf("threadMessageBody: %v", err)
	}
	var got struct {
		Text    string   `json:"text"`
		FileIDs []string `json:"file_ids"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got.Text != testMessageText || len(got.FileIDs) != 2 || got.FileIDs[1] != testFileID2 {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestThreadMessageBodyFromJSON(t *testing.T) {
	cmd := newThreadsMessagesCreateCmd()
	if err := cmd.Flags().Set(keyData, `{"text":"hello"}`); err != nil {
		t.Fatalf("set data: %v", err)
	}
	body, err := threadMessageBody(cmd)
	if err != nil {
		t.Fatalf("threadMessageBody: %v", err)
	}
	if got, want := string(body), `{"text":"hello"}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestThreadMessageBodyRejectsTextAndJSON(t *testing.T) {
	cmd := newThreadsMessagesCreateCmd()
	if err := cmd.Flags().Set("text", testMessageText); err != nil {
		t.Fatalf("set text: %v", err)
	}
	if err := cmd.Flags().Set(keyData, `{"text":"hello"}`); err != nil {
		t.Fatalf("set data: %v", err)
	}
	if _, err := threadMessageBody(cmd); err == nil {
		t.Fatalf("expected conflict error")
	}
}

func TestThreadMessageBodyRequiresTextWithFileIDs(t *testing.T) {
	cmd := newThreadsMessagesCreateCmd()
	if err := cmd.Flags().Set(threadMessageFileIDsFlag, testFileID1); err != nil {
		t.Fatalf("set file IDs: %v", err)
	}
	if _, err := threadMessageBody(cmd); err == nil {
		t.Fatalf("expected missing text error")
	}
}

func TestThreadMessageIdempotency(t *testing.T) {
	const key = "message-retry"
	const textKey = "text"
	const bodyJSON = `{"text":"hello"}`
	const messages = "messages"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/api/v1/public/threads/a%2Fb/messages" || r.Header.Get(headerIdempotency) != key {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		assertSkillJSON(t, string(body), bodyJSON)
		_, _ = w.Write([]byte(`{"id":"message-1"}`))
	}))
	defer server.Close()
	setDatabaseTestEnv(t, server.URL)
	for _, flags := range [][]string{{"--text", testMessageText}, {"--" + keyData, bodyJSON}} {
		root := newRootCmd()
		root.SetArgs(append([]string{"threads", messages, commandUseCreate, usageTestID, "--" + flagIdempotency, key}, flags...))
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	session := newTestMCPSession(t, client.New(server.URL, "test-key"))
	for _, bodyArgs := range []map[string]any{{textKey: testMessageText}, {keyData: map[string]any{textKey: testMessageText}}} {
		bodyArgs[keyThreadID], bodyArgs[keyIdempotency] = usageTestID, key
		result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: toolThreadsMessagesCreate, Arguments: bodyArgs})
		if err != nil {
			t.Fatal(err)
		}
		assertSkillJSON(t, toolResultText(t, result), `{"id":"message-1"}`)
	}
}
