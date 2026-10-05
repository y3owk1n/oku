package forge_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/forge"
)

// The CLI cannot reach a test server over https, so this checks the redirect
// rule that every download and version lookup shares.
func TestB419ARedirectFromHTTPSToAnotherSchemeIsRefused(t *testing.T) {
	from, err := http.NewRequest(http.MethodGet, "https://example.com/tool.tar.gz", nil)
	if err != nil {
		t.Fatal(err)
	}

	for target, refused := range map[string]bool{
		"http://example.com/tool.tar.gz":      true,
		"file:///etc/passwd":                  true,
		"https://cdn.example.com/tool.tar.gz": false,
	} {
		to, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}

		err = forge.CheckRedirect(to, []*http.Request{from})

		switch {
		case !refused && err != nil:
			t.Fatalf("a redirect to %s was refused: %v", target, err)
		case refused && (err == nil || !strings.Contains(err.Error(), from.URL.String()+" redirects to "+target)):
			t.Fatalf("want a redirect to %s refused naming both URLs, got %v", target, err)
		}
	}
}
