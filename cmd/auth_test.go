package cmd

import "testing"

func TestTokenPageURL(t *testing.T) {
	cases := []struct {
		baseURL, slug, want string
	}{
		{"https://app.interloom.com", "", "https://app.interloom.com/personal-tokens?autoCreate=true&name=Interloom%20CLI"},
		{"https://app.interloom.com/", "", "https://app.interloom.com/personal-tokens?autoCreate=true&name=Interloom%20CLI"},
		{"https://app.interloom.com", "acme", "https://app.interloom.com/acme/personal-tokens?autoCreate=true&name=Interloom%20CLI"},
		{"http://localhost:8080", "test-org", "http://localhost:8080/test-org/personal-tokens?autoCreate=true&name=Interloom%20CLI"},
	}
	for _, tc := range cases {
		if got := tokenPageURL(tc.baseURL, tc.slug); got != tc.want {
			t.Errorf("tokenPageURL(%q, %q) = %q, want %q", tc.baseURL, tc.slug, got, tc.want)
		}
	}
}

func TestOrgSlugFromUser(t *testing.T) {
	slug, err := orgSlugFromUser([]byte(`{"id":"u1","organization":{"id":"o1","name":"Acme","slug":"acme"}}`))
	if err != nil {
		t.Fatalf("orgSlugFromUser: %v", err)
	}
	if slug != "acme" {
		t.Errorf("slug = %q, want %q", slug, "acme")
	}

	if _, err := orgSlugFromUser([]byte(`{"id":"u1","organization":{}}`)); err == nil {
		t.Error("expected error when organization slug is missing")
	}
	if _, err := orgSlugFromUser([]byte(`not json`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
}
