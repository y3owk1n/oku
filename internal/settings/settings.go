// Package settings reads and writes per-user settings of the OS. Between the
// list, the ledger and a store a value is a fragment: a string in the store's
// own form that holds the type too, such as <integer>48</integer> for macOS or
// REG_DWORD:48 for the registry. So a value that oku read comes back as it was.
package settings

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Store is one mechanism of the OS that holds settings, such as the preference
// domains of macOS.
type Store interface {
	// Unavailable says why this machine cannot use the store, or is empty. oku
	// then skips the store's table and says so, because one list also serves a
	// machine without a desktop.
	Unavailable() string
	// Encode turns a value of the list into a fragment, or says why this store
	// cannot hold it.
	Encode(value any) (string, error)
	// Read returns the value of key in domain as a fragment, and whether the key
	// is set.
	Read(domain, key string) (string, bool, error)
	Write(domain, key, fragment string) error
	Delete(domain, key string) error
	// Applied tells the OS that settings in these domains changed. It runs once
	// after a change.
	Applied(domains []string)
}

// EncodePlist turns a value of the list into a fragment of an XML property
// list. A table keeps its keys in sorted order, so the same value always gives
// the same fragment.
func EncodePlist(value any) (string, error) {
	switch v := value.(type) {
	case bool:
		if v {
			return "<true/>", nil
		}

		return "<false/>", nil
	case int64:
		return "<integer>" + strconv.FormatInt(v, 10) + "</integer>", nil
	case float64:
		return "<real>" + strconv.FormatFloat(v, 'g', -1, 64) + "</real>", nil
	case string:
		return "<string>" + escape(v) + "</string>", nil
	case []any:
		var out strings.Builder

		for _, element := range v {
			fragment, err := EncodePlist(element)
			if err != nil {
				return "", err
			}

			out.WriteString(fragment)
		}

		return "<array>" + out.String() + "</array>", nil
	case map[string]any:
		var out strings.Builder

		for _, key := range slices.Sorted(maps.Keys(v)) {
			fragment, err := EncodePlist(v[key])
			if err != nil {
				return "", fmt.Errorf("%s: %w", key, err)
			}

			out.WriteString("<key>" + escape(key) + "</key>" + fragment)
		}

		return "<dict>" + out.String() + "</dict>", nil
	default:
		return "", fmt.Errorf(
			"a setting is a boolean, an integer, a float, a string, an array or a table, not %T",
			value,
		)
	}
}

func escape(s string) string {
	var out bytes.Buffer

	// EscapeText fails only when the writer does.
	_ = xml.EscapeText(&out, []byte(s))

	return out.String()
}

// Find returns the fragment of key in an exported property list, whose top
// level is a dict, and whether the key is there.
func Find(exported []byte, key string) (string, bool, error) {
	d := xml.NewDecoder(bytes.NewReader(exported))
	depth := 0
	found := false

	for {
		start := d.InputOffset()

		token, err := d.Token()
		if errors.Is(err, io.EOF) {
			return "", false, nil
		}

		if err != nil {
			return "", false, fmt.Errorf("read the exported settings: %w", err)
		}

		switch t := token.(type) {
		case xml.EndElement:
			depth--
		case xml.StartElement:
			depth++

			// The keys and values of the top dict are at depth 3, under plist and dict.
			if depth != 3 {
				continue
			}

			if found {
				if err := d.Skip(); err != nil {
					return "", false, fmt.Errorf("read the exported settings: %w", err)
				}

				return strings.TrimSpace(string(exported[start:d.InputOffset()])), true, nil
			}

			if t.Name.Local != "key" {
				if err := d.Skip(); err != nil {
					return "", false, fmt.Errorf("read the exported settings: %w", err)
				}

				depth--

				continue
			}

			var name string
			if err := d.DecodeElement(&name, &t); err != nil {
				return "", false, fmt.Errorf("read the exported settings: %w", err)
			}

			depth--
			found = name == key
		}
	}
}
