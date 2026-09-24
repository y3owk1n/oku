package resolve

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/y3owk1n/oku/internal/manifest"
)

// maxPage is the most of a page that version.from = "page" reads. An update
// feed or a download page is far smaller.
const maxPage = 8 << 20

// scrapedRe is what a version read from a redirect or a page may look like.
// It becomes part of store paths and download URLs.
var scrapedRe = regexp.MustCompile(`^[0-9][0-9A-Za-z._+-]*$`)

// scrape returns the one release that a redirect or a page names. The groups of
// version.regex, joined with ".", are the version, so a regex can read
// "host_version":[0,0,413] as 0.0.413.
func (r *Resolver) scrape(ctx context.Context, v manifest.Version) (Release, error) {
	what := "read the version from " + v.Repo

	re, err := regexp.Compile(v.Regex)
	if err != nil {
		return Release{}, fmt.Errorf("%s: version.regex: %w", what, err)
	}

	text, err := r.fetchText(ctx, v, re)
	if err != nil {
		return Release{}, fmt.Errorf("%s: %w", what, err)
	}

	match := re.FindStringSubmatch(text)
	if match == nil {
		return Release{}, fmt.Errorf("%s: version.regex %s matches nothing in %s", what, v.Regex, quoted(text))
	}

	var parts []string

	for _, group := range match[1:] {
		if group != "" {
			parts = append(parts, group)
		}
	}

	version := strings.Join(parts, ".")
	if !scrapedRe.MatchString(version) {
		return Release{}, fmt.Errorf(
			"%s: version.regex %s captured %q, which is not a version", what, v.Regex, version,
		)
	}

	return Release{Version: version, Tag: version}, nil
}

// maxHops is how many redirects "redirect" follows before it gives up.
const maxHops = 10

// fetchText returns the body of the page at v.Repo, or the URLs its redirects
// lead to, one per line. It follows redirects until one leads to a URL that re
// matches, so it downloads no file when one does.
func (r *Resolver) fetchText(ctx context.Context, v manifest.Version, re *regexp.Regexp) (string, error) {
	var hops []string

	client := r.Hosts.HTTP
	if v.From == manifest.FromRedirect {
		// Not r.Hosts.HTTP, whose cache would keep the download at the end of
		// redirects that regex never matches.
		client = &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
			hops = append(hops, req.URL.String())
			if re.MatchString(req.URL.String()) || len(via) >= maxHops {
				return http.ErrUseLastResponse
			}

			return nil
		}}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.Repo, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "oku")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if v.From == manifest.FromRedirect {
		if len(hops) == 0 {
			return "", fmt.Errorf("the server answered %s, not a redirect", resp.Status)
		}

		return strings.Join(hops, "\n"), nil
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the server answered %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPage+1))
	if err != nil {
		return "", err
	}

	if len(body) > maxPage {
		return "", fmt.Errorf("the page is larger than %d bytes", maxPage)
	}

	return string(body), nil
}

// quoted shows the start of text in an error.
func quoted(text string) string {
	const most = 200

	if len(text) > most {
		return fmt.Sprintf("%q...", text[:most])
	}

	return fmt.Sprintf("%q", text)
}

var (
	sparkleItemRe = regexp.MustCompile(`(?s)<item\b.*?</item>`)
	// A feed puts the version for people in an element or in an attribute of
	// the enclosure.
	sparkleShortRe = regexp.MustCompile(
		`sparkle:shortVersionString\s*(?:=\s*"([^"]*)"|>\s*([^<]*?)\s*</sparkle:shortVersionString>)`,
	)
)

// sparkle returns the newest macOS release of the Sparkle appcast at v.Repo by
// its sparkle:shortVersionString. An item on a channel, such as beta, or for
// another system, is skipped.
func (r *Resolver) sparkle(ctx context.Context, v manifest.Version) (Release, error) {
	what := "read the versions of " + v.Repo

	text, err := r.fetchText(ctx, v, nil)
	if err != nil {
		return Release{}, fmt.Errorf("%s: %w", what, err)
	}

	newest := ""

	for _, item := range sparkleItemRe.FindAllString(text, -1) {
		// A feed that WinSparkle shares marks the items of other systems.
		m := sparkleShortRe.FindStringSubmatch(item)
		if m == nil || strings.Contains(item, "<sparkle:channel>") ||
			strings.Contains(item, `sparkle:os="`) && !strings.Contains(item, `sparkle:os="macos"`) {
			continue
		}

		if version := m[1] + m[2]; scrapedRe.MatchString(version) && (newest == "" || Compare(version, newest) > 0) {
			newest = version
		}
	}

	if newest == "" {
		return Release{}, fmt.Errorf("%s: no item names a sparkle:shortVersionString", what)
	}

	return Release{Version: newest, Tag: newest}, nil
}
