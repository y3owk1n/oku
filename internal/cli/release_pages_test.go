package cli_test

import (
	"fmt"
	"testing"
)

func TestB386ALookupReadsTheNewestPageOfReleasesAndNoMore(t *testing.T) {
	m := newMachine(t)

	// 150 releases, so the host has a second page.
	var tags []string
	for i := 150; i > 0; i-- {
		tags = append(tags, fmt.Sprintf("v2.0.%d", i))
	}

	server := newReleaseServer(t, tags...)
	m.opts.GitHubAPI = server.URL + "/api"

	_, err := m.run(t, "", "add", m.discoveredManifest(t, "2.0.150"))
	must(t, err)

	if got := m.toolOutput(t); got != "2.0.150" {
		t.Fatalf("add installed %s, want 2.0.150", got)
	}

	if server.hits != 1 {
		t.Fatalf("add asked for %d pages of releases, want 1", server.hits)
	}
}

func TestB386ALookupReadsTheOtherPagesWhenTheNewestHoldsNoVersionItWants(t *testing.T) {
	m := newMachine(t)

	// The newest 100 releases are a nightly stream, so the version oku wants is
	// on a later page.
	var tags []string
	for i := 120; i > 0; i-- {
		tags = append(tags, fmt.Sprintf("nightly-%d", i))
	}

	tags = append(tags, "v1.2.0")

	server := newReleaseServer(t, tags...)
	m.opts.GitHubAPI = server.URL + "/api"

	_, err := m.run(t, "", "add", m.discoveredManifest(t, "1.2.0"))
	must(t, err)

	if got := m.toolOutput(t); got != "1.2.0" {
		t.Fatalf("add installed %s, want 1.2.0 from a later page", got)
	}

	if server.hits < 2 {
		t.Fatalf("add asked for %d pages, want every page of the list", server.hits)
	}
}
