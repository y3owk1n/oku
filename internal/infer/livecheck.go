package infer

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/y3owk1n/oku/internal/manifest"
)

// A livecheck block of a cask that reads fields of a JSON or an XML feed, and
// returns them joined with commas, translates into version.json or a regex.
// oku reads the block and runs none of it.
var (
	// liveMapRe opens a loop over a list of the feed.
	liveMapRe = regexp.MustCompile(`^json((?:\["[^"]+"\]|\.dig\([^)]*\))+)&?\.map do \|(\w+)\|$`)
	// liveAssignRe gives a name the value of an expression.
	liveAssignRe = regexp.MustCompile(`^(\w+)\s*=\s*(.+)$`)
	// liveFieldRe is a field of the feed or of an item of its list, with the
	// match of a regex over it.
	liveFieldRe = regexp.MustCompile(
		`^(\w+)((?:\["[^"]+"\]|\.dig\([^)]*\))+)(&?\.match\(regex\))?$`,
	)
	liveKeyRe    = regexp.MustCompile(`\["([^"]+)"\]|\.dig\(([^)]*)\)`)
	liveResultRe = regexp.MustCompile(`^"((?:#\{[^}]+\},?)+)"$`)
	liveRefRe    = regexp.MustCompile(`#\{([^}]+)\}`)
	liveMatchRe  = regexp.MustCompile(`^(\w+)\[(\d+)\]$`)
	// liveOneRe is a whole block of one line: a field of the feed, or a field of
	// each item of a list, as in json["releases"]&.map { |r| r["version"] }. A
	// list value joined with dots is the list itself, which the page source
	// joins with dots.
	liveOneRe = regexp.MustCompile(
		`^json((?:\["[^"]+"\]|\.dig\([^)]*\))+)(?:&?\.map \{ \|(\w+)\| (\w+)((?:\["[^"]+"\]|\.dig\([^)]*\))+) \})?(&?\.join\("\."\))?$`,
	)
	// liveXMLRe is the value of a key of a property list.
	liveXMLRe = regexp.MustCompile(`^xml\.elements\["//key\[text\(\)='([^']+)'\]"\]&\.next_element&\.text$`)
)

// liveField is where a part of the version comes from.
type liveField struct {
	path  string
	match bool
}

// blockFollow translates the body of a livecheck with a JSON or an XML
// strategy block into a page source at repo, or returns nil. regex is the
// livecheck's own regex, which a match in the block applies, and parts is how
// many parts the cask's version has.
func blockFollow(body, strategy, repo, regex string, parts int) *follow {
	block, ok := strategyBlock(body)
	if !ok {
		return nil
	}

	if len(block) == 1 && parts == 1 && strategy == "json" {
		// A single key, json["version"], stays the regex the translation always
		// wrote for it, so its manifest does not change.
		m := liveOneRe.FindStringSubmatch(block[0])
		if m == nil || m[2] != m[3] || m[2] == "" && m[5] == "" && !strings.Contains(m[1], ".dig(") &&
			strings.Count(m[1], "[") == 1 {
			return nil
		}

		path := jsonPath(m[1])
		if m[2] != "" {
			path += ".*." + jsonPath(m[4])
		}

		return &follow{from: manifest.FromPage, repo: repo, json: path}
	}

	var (
		prefix string
		item   string
		fields = map[string]liveField{}
		result string
	)

	for _, line := range block {
		switch m := liveMapRe.FindStringSubmatch(line); {
		case m != nil && strategy == "json":
			prefix, item = jsonPath(m[1])+".*", m[2]

			continue
		case line == "end" || strings.HasPrefix(line, "next"):
			continue
		}

		if m := liveAssignRe.FindStringSubmatch(line); m != nil {
			f, ok := liveValue(m[2], strategy, prefix, item)
			if !ok {
				return nil
			}

			fields[m[1]] = f

			continue
		}

		if m := liveResultRe.FindStringSubmatch(line); m != nil {
			result = m[1]

			continue
		}

		// Any other line computes something oku does not read.
		return nil
	}

	if len(liveRefRe.FindAllString(result, -1)) != parts {
		return nil
	}

	return liveFollow(result, fields, strategy, repo, regex)
}

// liveRegex turns a Ruby regex of a livecheck into Go's syntax, with its flags
// kept to itself, since a block's regex goes into a larger one.
func liveRegex(body, flags string) (string, bool) {
	if strings.Contains(body, "#{") || strings.Trim(flags, "im") != "" {
		return "", false
	}

	body = strings.ReplaceAll(strings.ReplaceAll(body, `\/`, "/"), `\h`, `[0-9a-fA-F]`)
	flags = strings.ReplaceAll(flags, "m", "s")

	if flags != "" {
		body = "(?" + flags + ":" + body + ")"
	}

	_, err := regexp.Compile(body)

	return body, err == nil
}

// liveFollow builds the page source from the parts that result names.
func liveFollow(result string, fields map[string]liveField, strategy, repo, regex string) *follow {
	if result == "" {
		return nil
	}

	var (
		paths   []liveField
		groups  = map[string]int{}
		matches int
	)

	for _, ref := range liveRefRe.FindAllStringSubmatch(result, -1) {
		name := strings.TrimSuffix(ref[1], ".strip")
		n := 0

		if m := liveMatchRe.FindStringSubmatch(name); m != nil {
			name, n = m[1], atoi(m[2])
		}

		f, ok := fields[name]
		if !ok || f.match != (n > 0) {
			return nil
		}

		// The groups of a match must come in order, one after the other.
		if n > 0 {
			if groups[name] != n-1 {
				return nil
			}

			groups[name] = n
			matches++
		}

		if n <= 1 {
			paths = append(paths, f)
		}
	}

	f := &follow{from: manifest.FromPage, repo: repo, join: "+"}

	var lines, jsonPaths []string

	for _, p := range paths {
		jsonPaths = append(jsonPaths, p.path)

		if p.match {
			lines = append(lines, `[^\n]*?`+regex+`[^\n]*`)
		} else {
			lines = append(lines, `([^\n]*)`)
		}
	}

	f.json = strings.Join(jsonPaths, "\n")

	switch {
	case matches == 0:
	case len(paths) == 1:
		f.regex = regex
	default:
		f.regex = `^` + strings.Join(lines, `\n`)
	}

	// A match must use every group of the regex, or the version gets parts it
	// does not have.
	if matches > 0 {
		re, err := regexp.Compile(regex)
		if err != nil || re.NumSubexp() != matches {
			return nil
		}
	}

	if len(paths) == 1 && matches == 0 {
		f.join = ""
	}

	return f
}

// liveValue reads the expression of an assignment in a livecheck block.
func liveValue(expr, strategy, prefix, item string) (liveField, bool) {
	// Of alternatives, oku reads the first.
	expr, _, _ = strings.Cut(expr, " || ")
	expr = strings.TrimSpace(expr)

	if strategy == "xml" {
		m := liveXMLRe.FindStringSubmatch(expr)

		return liveField{path: m[1]}, m != nil
	}

	m := liveFieldRe.FindStringSubmatch(expr)
	if m == nil {
		return liveField{}, false
	}

	path := jsonPath(m[2])

	switch m[1] {
	case "json":
	case item:
		path = prefix + "." + path
	default:
		return liveField{}, false
	}

	return liveField{path: path, match: m[3] != ""}, true
}

// jsonPath turns the keys of ["a"]["b"] or .dig("a", "b") into a.b.
func jsonPath(access string) string {
	var keys []string

	for _, m := range liveKeyRe.FindAllStringSubmatch(access, -1) {
		if m[1] != "" {
			keys = append(keys, m[1])

			continue
		}

		for _, key := range strings.Split(m[2], ",") {
			keys = append(keys, strings.Trim(strings.TrimSpace(key), `"`))
		}
	}

	return strings.Join(keys, ".")
}

// strategyBlock returns the lines of the block of a strategy in body, and
// whether it has one.
func strategyBlock(body string) ([]string, bool) {
	var (
		out   []string
		depth int
	)

	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)

		switch {
		case depth == 0 && strings.HasPrefix(line, "strategy ") && strings.Contains(line, " do |"):
			depth = 1
		case depth == 0:
		case line == "end" && depth == 1:
			return out, true
		default:
			if strings.HasSuffix(line, " do") || strings.Contains(line, " do |") {
				depth++
			} else if line == "end" {
				depth--
			}

			if line != "" {
				out = append(out, line)
			}
		}
	}

	return nil, false
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)

	return n
}
