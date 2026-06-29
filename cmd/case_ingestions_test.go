package cmd

import "testing"

func TestCaseIngestionsCommandShape(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{resourceCaseIngestions})
	if err != nil || cmd == nil || cmd.Use != resourceCaseIngestions {
		t.Fatalf("case-ingestions command not registered: child=%v err=%v", cmd, err)
	}

	create, _, err := root.Find([]string{resourceCaseIngestions, commandUseCreate, "manifest.jsonl"})
	if err != nil || create == nil || create.Flags().Lookup("space-id") == nil {
		t.Fatalf("case-ingestions create shape invalid: child=%v err=%v", create, err)
	}

	errors, _, err := root.Find([]string{resourceCaseIngestions, "errors", "ingestion-1"})
	if err != nil || errors == nil || errors.Use != "errors <id>" {
		t.Fatalf("case-ingestions errors command not registered: child=%v err=%v", errors, err)
	}
	for _, flag := range []string{argLimit, keyCursor, argAll} {
		if errors.Flags().Lookup(flag) == nil {
			t.Fatalf("case-ingestions errors should expose --%s", flag)
		}
	}
}
