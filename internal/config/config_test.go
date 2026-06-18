package config

import "testing"

const (
	devName   = "dev"
	devURL    = "https://dev.interloom.com"
	localURL  = "http://localhost:8080"
	localName = "localhost-8080"
	orgSlug   = "acme"
	appName   = "app"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantURL  string
	}{
		{devName, devName, devURL},
		{"dev.interloom.com", devName, devURL},
		{"https://dev.interloom.com/", devName, devURL},
		{"DEV.Interloom.com", devName, devURL},
		{"staging.eu.interloom.com", "staging.eu", "https://staging.eu.interloom.com"},
		{"localhost:8080", localName, localURL},
		{"http://localhost:8080", localName, localURL},
		{"https://localhost:3000", "localhost-3000", "http://localhost:3000"}, // local forces http
		{"127.0.0.1:9000", "127.0.0.1-9000", "http://127.0.0.1:9000"},
		{"interloom.com", "interloom.com", "https://interloom.com"},
		{"api.example.com", "api.example.com", "https://api.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			name, url, err := Normalize(tc.in)
			if err != nil {
				t.Fatalf("Normalize(%q) error: %v", tc.in, err)
			}
			if name != tc.wantName {
				t.Errorf("name = %q, want %q", name, tc.wantName)
			}
			if url != tc.wantURL {
				t.Errorf("url = %q, want %q", url, tc.wantURL)
			}
		})
	}
}

func TestNormalizeEmpty(t *testing.T) {
	if _, _, err := Normalize("  "); err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestInstanceName(t *testing.T) {
	cases := []struct {
		host, slug, want string
	}{
		{appName, orgSlug, "app-acme"},
		{appName, "beta", "app-beta"},
		{devName, "test-org", "dev-test-org"},
		{localName, orgSlug, "localhost-8080-acme"},
		{appName, "", appName}, // slug unknown
	}
	for _, tc := range cases {
		if got := InstanceName(tc.host, tc.slug); got != tc.want {
			t.Errorf("InstanceName(%q, %q) = %q, want %q", tc.host, tc.slug, got, tc.want)
		}
	}
}

func TestInstanceRoundtrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	in := Instance{APIKey: "secret-key", BaseURL: devURL, OrganizationSlug: orgSlug}
	if err := SaveInstance(devName, in); err != nil {
		t.Fatalf("SaveInstance: %v", err)
	}
	got, err := LoadInstance(devName)
	if err != nil {
		t.Fatalf("LoadInstance: %v", err)
	}
	if got != in {
		t.Errorf("roundtrip = %+v, want %+v", got, in)
	}

	if cur := CurrentInstance(); cur != "" {
		t.Errorf("CurrentInstance before set = %q, want empty", cur)
	}
	if err := SetCurrentInstance(devName); err != nil {
		t.Fatalf("SetCurrentInstance: %v", err)
	}
	if cur := CurrentInstance(); cur != devName {
		t.Errorf("CurrentInstance = %q, want %q", cur, devName)
	}
}

func TestResolvePrecedence(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SaveInstance(devName, Instance{APIKey: "file-key", BaseURL: devURL}); err != nil {
		t.Fatal(err)
	}
	if err := SetCurrentInstance(devName); err != nil {
		t.Fatal(err)
	}

	// Env overrides the file value for the api key.
	t.Setenv(EnvAPIKey, "env-key")
	r, err := Resolve("", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.APIKey != "env-key" {
		t.Errorf("APIKey = %q, want env-key (env overrides file)", r.APIKey)
	}
	if r.BaseURL != devURL {
		t.Errorf("BaseURL = %q, want file value", r.BaseURL)
	}

	// Flag overrides env/file for the base URL.
	r, err = Resolve("", "https://override.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if r.BaseURL != "https://override.example.com" {
		t.Errorf("BaseURL = %q, want flag override", r.BaseURL)
	}
}
