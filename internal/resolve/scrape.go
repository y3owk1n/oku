package resolve

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

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

	if len(v.JSON) > 0 {
		return jsonVersion(text, v, re, what)
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

	version := strings.Join(parts, cmp.Or(v.Join, "."))
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
	sparkleBuildRe = regexp.MustCompile(
		`sparkle:version\s*(?:=\s*"([^"]*)"|>\s*([^<]*?)\s*</sparkle:version>)`,
	)
)

// sparkle returns the newest macOS release of the Sparkle appcast at v.Repo,
// its sparkle:shortVersionString, and its sparkle:version after v.Join when
// that is set. It skips an item on a channel, such as beta, or for another
// system.
func (r *Resolver) sparkle(ctx context.Context, v manifest.Version) (Release, error) {
	what := "read the versions of " + v.Repo

	text, err := r.fetchText(ctx, v, nil)
	if err != nil {
		return Release{}, fmt.Errorf("%s: %w", what, err)
	}

	// Sparkle takes the item with the highest build, sparkle:version, as the
	// newest. oku does the same, and compares short versions only when an item
	// names no build. An old item may give a commit as its short version.
	var (
		newest, newestBuild string
		published           time.Time
	)

	for _, item := range sparkleItemRe.FindAllString(text, -1) {
		// A feed that WinSparkle shares marks the items of other systems.
		m := sparkleShortRe.FindStringSubmatch(item)
		if m == nil || offChannel(item) ||
			strings.Contains(item, `sparkle:os="`) && !strings.Contains(item, `sparkle:os="macos"`) {
			continue
		}

		// A feed may add the build to the short version, as "1.165.1 (87405)".
		version, _, _ := strings.Cut(strings.TrimSpace(m[1]+m[2]), " ")

		build := ""
		if b := sparkleBuildRe.FindStringSubmatch(item); b != nil {
			build = strings.TrimSpace(b[1] + b[2])
		}

		// With join, the version is the short version and the build, such as
		// "1.2+345".
		if v.Join != "" {
			if build == "" {
				continue
			}

			version += v.Join + build
		}

		if !scrapedRe.MatchString(version) {
			continue
		}

		var newer bool

		switch {
		case newest == "":
			newer = true
		case build != "" && newestBuild != "":
			newer = Compare(build, newestBuild) > 0
		default:
			newer = Compare(version, newest) > 0
		}

		if newer {
			newest, newestBuild, published = version, build, pubDate(item)
		}
	}

	if newest == "" {
		return Release{}, fmt.Errorf("%s: no item names a sparkle:shortVersionString", what)
	}

	return Release{Version: newest, Tag: newest, Published: published}, nil
}

var sparklePubDateRe = regexp.MustCompile(`<pubDate>\s*([^<]*?)\s*</pubDate>`)

// pubDate reads the RSS pubDate of a Sparkle item, when the feed wrote one in
// a form that RSS allows, or returns the zero time.
func pubDate(item string) time.Time {
	m := sparklePubDateRe.FindStringSubmatch(item)
	if m == nil {
		return time.Time{}
	}

	for _, layout := range []string{
		time.RFC1123Z, time.RFC1123, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST",
	} {
		if at, err := time.Parse(layout, m[1]); err == nil {
			return at
		}
	}

	return time.Time{}
}

var sparkleChannelRe = regexp.MustCompile(`<sparkle:channel>\s*([^<]*?)\s*</sparkle:channel>`)

// offChannel reports whether a Sparkle item is on a channel that users opt
// into, such as beta. Some feeds, such as OrbStack's, put every release on a
// channel named stable or release, which counts as no channel.
func offChannel(item string) bool {
	m := sparkleChannelRe.FindStringSubmatch(item)

	return m != nil && !slices.Contains([]string{"stable", "release"}, strings.ToLower(m[1]))
}

// jsonVersion reads the version from the JSON in text at the paths of
// v.JSON. Their values, one per line, are what re reads, or without a regex
// the parts of the version. A path with "*" gives one candidate per item of
// its list, and the newest version wins.
func jsonVersion(text string, v manifest.Version, re *regexp.Regexp, what string) (Release, error) {
	var doc any

	// A feed is JSON, or an XML property list, as Apple's feeds are.
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		var plistErr error
		if doc, plistErr = parsePlist(text); plistErr != nil {
			return Release{}, fmt.Errorf(
				"%s: version.json needs a JSON answer or a property list: %w", what, err,
			)
		}
	}

	// The list that the paths with "*" go through, and its length.
	items := 1

	for _, p := range v.JSON {
		if before, _, ok := strings.Cut(p, "*"); ok {
			list, _ := jsonAt(doc, strings.TrimSuffix(before, "."))
			values, _ := list.([]any)
			items = len(values)
		}
	}

	var best string

	for i := range items {
		var values []string

		for _, p := range v.JSON {
			value, ok := jsonAt(doc, strings.Replace(p, "*", strconv.Itoa(i), 1))
			if !ok {
				values = nil

				break
			}

			// A list, such as [0, 0, 413], is its items joined with dots.
			if list, ok := value.([]any); ok {
				items := make([]string, len(list))
				for j, item := range list {
					items[j] = fmt.Sprint(item)
				}

				value = strings.Join(items, ".")
			}

			values = append(values, fmt.Sprint(value))
		}

		version := ""

		switch {
		case values == nil:
		case v.Regex == "":
			version = strings.Join(values, cmp.Or(v.Join, "."))
		default:
			if m := re.FindStringSubmatch(strings.Join(values, "\n")); m != nil {
				var parts []string

				for _, group := range m[1:] {
					if group != "" {
						parts = append(parts, group)
					}
				}

				version = strings.Join(parts, cmp.Or(v.Join, "."))
			}
		}

		if scrapedRe.MatchString(version) && (best == "" || Compare(version, best) > 0) {
			best = version
		}
	}

	if best == "" {
		return Release{}, fmt.Errorf("%s: version.json %s gives no version", what, strings.Join(v.JSON, ", "))
	}

	return Release{Version: best, Tag: best}, nil
}

// jsonAt returns the value at path in doc, keys and list indexes joined by
// dots, and whether it is there and not null.
func jsonAt(doc any, path string) (any, bool) {
	at := doc

	for _, part := range strings.Split(path, ".") {
		switch node := at.(type) {
		case map[string]any:
			at = node[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}

			at = node[i]
		default:
			return nil, false
		}
	}

	return at, at != nil
}
