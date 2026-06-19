package cmd

import "testing"

func TestThreadMessageBodyFromText(t *testing.T) {
	cmd := newThreadsMessageCmd()
	if err := cmd.Flags().Set("text", "hello"); err != nil {
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

func TestThreadMessageBodyFromJSON(t *testing.T) {
	cmd := newThreadsMessageCmd()
	if err := cmd.Flags().Set("data", `{"text":"hello"}`); err != nil {
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
	cmd := newThreadsMessageCmd()
	if err := cmd.Flags().Set("text", "hello"); err != nil {
		t.Fatalf("set text: %v", err)
	}
	if err := cmd.Flags().Set("data", `{"text":"hello"}`); err != nil {
		t.Fatalf("set data: %v", err)
	}
	if _, err := threadMessageBody(cmd); err == nil {
		t.Fatalf("expected conflict error")
	}
}
