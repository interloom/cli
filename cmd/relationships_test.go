package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/interloom/cli/internal/config"
)

func TestRelationshipsCommandShape(t *testing.T) {
	root := newRootCmd()
	for _, resourceName := range []string{
		resourceSpaces,
		resourceCases,
		"notes",
		"procedures",
		"agents",
		"files",
		"users",
	} {
		cmd, _, err := root.Find([]string{resourceName, commandNameRelationships, "resource-1"})
		if err != nil || cmd == nil || cmd.Use != commandNameRelationships+" <id>" {
			t.Fatalf("%s relationships command not registered: child=%v err=%v", resourceName, cmd, err)
		}
		for _, flag := range []string{argLimit, keyCursor, argAll} {
			if cmd.Flags().Lookup(flag) == nil {
				t.Fatalf("%s relationships should expose --%s", resourceName, flag)
			}
		}
	}
}

func TestRelationshipsCommandOnlyExistsForSupportedResources(t *testing.T) {
	root := newRootCmd()
	for _, resourceName := range []string{resourceModels, resourceTools, resourceSecrets} {
		cmd, _, err := root.Find([]string{resourceName, commandNameRelationships, "resource-1"})
		if err == nil && cmd != nil && cmd.Name() == commandNameRelationships {
			t.Fatalf("%s should not expose relationships", resourceName)
		}
	}
}

func TestRelationshipsCommandSendsPaginatedRequest(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodGet; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/api/v1/public/cases/"+testCaseID+"/relationships"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get(argLimit), "2"; got != want {
			t.Errorf("limit = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get(keyCursor), "next-page"; got != want {
			t.Errorf("cursor = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer test-key"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"data":[],"has_more":false}`))
	}))
	defer apiServer.Close()

	t.Setenv(config.EnvAPIKey, "test-key")
	t.Setenv(config.EnvBaseURL, apiServer.URL)
	t.Setenv(config.EnvConfig, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := newRootCmd()
	root.SetArgs([]string{
		resourceCases,
		commandNameRelationships,
		testCaseID,
		"--limit", "2",
		"--cursor", "next-page",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute relationships command: %v", err)
	}
}
