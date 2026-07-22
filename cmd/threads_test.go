package cmd

import (
	"encoding/json"
	"testing"
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
