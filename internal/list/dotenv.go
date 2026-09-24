package list

import (
	"fmt"
	"strings"

	"github.com/y3owk1n/oku/internal/manifest"
)

// DotenvVar is one variable of a .env file.
type DotenvVar struct {
	Name, Value string
}

// ParseDotenv reads a .env file, in the syntax most tools share:
//
//	# a comment
//	export NAME=value
//	NAME="a \"quoted\" value, over
//	several lines, with ${OTHER} and $OTHER"
//	NAME='literal $text'
//
// A value without quotes ends at the line or at " #". Double quotes take the
// escapes \n, \t, \", \\ and \$. A double-quoted or bare value expands
// ${NAME}, ${NAME:-default} and $NAME, from the file's earlier lines and then
// from lookup. Single quotes keep the value as it is.
func ParseDotenv(data []byte, lookup func(string) (string, bool)) ([]DotenvVar, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")

	var vars []DotenvVar

	own := map[string]string{}
	read := func(name string) (string, bool) {
		if value, ok := own[name]; ok {
			return value, true
		}

		return lookup(name)
	}

	line := 1

	for text != "" {
		var entry string

		entry, text, _ = strings.Cut(text, "\n")
		at := line
		line++

		entry = strings.TrimSpace(entry)
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}

		entry = strings.TrimPrefix(entry, "export ")

		name, value, ok := strings.Cut(entry, "=")
		name = strings.TrimSpace(name)

		switch {
		case !ok:
			return nil, fmt.Errorf("line %d: want NAME=value", at)
		case !manifest.ValidEnvName(name):
			return nil, fmt.Errorf("line %d: %q is not a variable name", at, name)
		}

		value = strings.TrimLeft(value, " \t")

		switch {
		case strings.HasPrefix(value, "'"), strings.HasPrefix(value, `"`):
			quote := value[:1]
			body := value[1:]

			// A quoted value may go on over the next lines.
			for closing(body, quote) < 0 && text != "" {
				var next string

				next, text, _ = strings.Cut(text, "\n")
				line++
				body += "\n" + next
			}

			end := closing(body, quote)
			if end < 0 {
				return nil, fmt.Errorf("line %d: the %s quote does not close", at, quote)
			}

			if rest := strings.TrimSpace(body[end+1:]); rest != "" && !strings.HasPrefix(rest, "#") {
				return nil, fmt.Errorf("line %d: %s after the closing quote", at, rest)
			}

			value = body[:end]
			if quote == `"` {
				value = expandDotenv(unescape(value), read)
			}
		default:
			if i := strings.Index(value, " #"); i >= 0 {
				value = value[:i]
			}

			value = expandDotenv(strings.TrimSpace(value), read)
		}

		own[name] = value
		vars = append(vars, DotenvVar{Name: name, Value: value})
	}

	return vars, nil
}

// closing returns the index of the quote that closes body, or -1. Inside
// double quotes a backslash escapes the next character.
func closing(body, quote string) int {
	for i := 0; i < len(body); i++ {
		switch {
		case quote == `"` && body[i] == '\\':
			i++
		case body[i] == quote[0]:
			return i
		}
	}

	return -1
}

// unescape turns the escapes of a double-quoted value into their characters.
// It keeps \$ for expandDotenv, which reads it as a plain $.
func unescape(value string) string {
	var out strings.Builder

	for i := 0; i < len(value); i++ {
		if value[i] != '\\' || i+1 == len(value) {
			out.WriteByte(value[i])

			continue
		}

		i++

		switch value[i] {
		case 'n':
			out.WriteByte('\n')
		case 't':
			out.WriteByte('\t')
		case '$':
			out.WriteString(`\$`)
		default:
			out.WriteByte(value[i])
		}
	}

	return out.String()
}

// expandDotenv expands ${NAME}, ${NAME:-default} and $NAME in value with read.
// \$ is a plain $.
func expandDotenv(value string, read func(string) (string, bool)) string {
	var out strings.Builder

	for i := 0; i < len(value); i++ {
		switch {
		case strings.HasPrefix(value[i:], `\$`):
			out.WriteByte('$')
			i++
		case value[i] != '$':
			out.WriteByte(value[i])
		case strings.HasPrefix(value[i:], "${"):
			end := strings.IndexByte(value[i:], '}')
			if end < 0 {
				out.WriteString(value[i:])

				return out.String()
			}

			name, fallback, hasDefault := strings.Cut(value[i+2:i+end], ":-")
			got, ok := read(name)

			if (!ok || got == "") && hasDefault {
				got = fallback
			}

			out.WriteString(got)
			i += end
		default:
			n := 1
			for i+n < len(value) && isNameByte(value[i+n], n == 1) {
				n++
			}

			if n == 1 {
				out.WriteByte('$')

				continue
			}

			got, _ := read(value[i+1 : i+n])
			out.WriteString(got)
			i += n - 1
		}
	}

	return out.String()
}

func isNameByte(c byte, first bool) bool {
	return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || !first && c >= '0' && c <= '9'
}
