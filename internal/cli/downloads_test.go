package cli_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestB325ADownloadThatIsNoArchiveMustBeAProgram(t *testing.T) {
	m := newMachine(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gitea/tea", "/dl/tool":
			// A forge serves the page of a repo, or a login page, with status 200.
			fmt.Fprint(w, "\n<!DOCTYPE html>\n<html><head><title>tea</title></head></html>\n")
		case "/dl/notes":
			fmt.Fprint(w, "just some notes\n")
		case "/dl/broken.gz":
			// A gzip header over data that does not decompress.
			fmt.Fprint(w, "\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\x03not deflate data at all")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	host, _ := url.Parse(server.URL)

	_, err := m.run(t, "", "add", server.URL+"/gitea/tea")
	if err == nil || !strings.Contains(err.Error(), "a web page") ||
		!strings.Contains(err.Error(), "gitea:"+host.Host+"/gitea/tea") {
		t.Fatalf("want the page of a repo refused with its gitea: ref, got %v", err)
	}

	_, err = m.run(t, "", "add", server.URL+"/dl/notes")
	if err == nil || !strings.Contains(err.Error(), "no archive and no program") {
		t.Fatalf("want a text file refused, got %v", err)
	}

	_, err = m.run(t, "", "add", server.URL+"/dl/broken.gz")
	if err == nil || !strings.Contains(err.Error(), "decompress") {
		t.Fatalf("want a download that does not decompress refused with the reason, got %v", err)
	}

	// A manifest whose download turns into a web page installs nothing.
	ref := m.rawManifest(t, "tool", fmt.Sprintf(
		"[[artifact]]\nurl = %q\nbin = [\"tool\"]\n", server.URL+"/dl/tool",
	))

	_, err = m.run(t, "", "add", ref)
	if err == nil || !strings.Contains(err.Error(), "a web page") {
		t.Fatalf("want the manifest's page refused, got %v", err)
	}

	if _, err := os.Stat(m.profile("bin", "tool")); err == nil {
		t.Fatal("a web page was installed as a program")
	}

	list, _ := os.ReadFile(filepath.Join(m.config, "oku.toml"))
	if strings.Contains(string(list), "tea") || strings.Contains(string(list), "notes") {
		t.Fatalf("a refused download reached the list:\n%s", list)
	}
}

func TestB327AURLWhoseFileNamesNoVersionTakesItFromItsFolder(t *testing.T) {
	m := newMachine(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/download/jq-1.8.1/jq-program", "/files/latest/tool":
			fmt.Fprint(w, "#!/bin/sh\necho hi\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	out, err := m.run(t, "", "add", server.URL+"/releases/download/jq-1.8.1/jq-program")
	if err != nil || !strings.Contains(out, "added jq-program 1.8.1") {
		t.Fatalf("want the version of the folder, got %v:\n%s", err, out)
	}

	// No folder names a version, so the version stays 0.
	out, err = m.run(t, "", "add", server.URL+"/files/latest/tool")
	if err != nil || !strings.Contains(out, "added tool 0") {
		t.Fatalf("want version 0, got %v:\n%s", err, out)
	}
}
