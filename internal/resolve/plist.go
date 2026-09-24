package resolve

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// errEnd is the end of a dict or an array, where plistValue finds no value.
var errEnd = errors.New("end of the container")

// parsePlist reads an XML property list into the values that encoding/json
// gives: a dict is a map, an array a slice, and every other value its text.
func parsePlist(text string) (any, error) {
	dec := xml.NewDecoder(strings.NewReader(text))
	// A feed names the DTD of Apple, which oku does not fetch.
	dec.Strict = false

	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}

		if start, ok := tok.(xml.StartElement); ok && start.Name.Local == "plist" {
			return plistValue(dec)
		}
	}
}

// plistValue reads the next value of dec. It returns errEnd at the end of the
// dict or the array that holds it.
func plistValue(dec *xml.Decoder) (any, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}

		switch t := tok.(type) {
		case xml.EndElement:
			return nil, errEnd
		case xml.StartElement:
			return plistElement(dec, t)
		}
	}
}

func plistElement(dec *xml.Decoder, start xml.StartElement) (any, error) {
	switch start.Name.Local {
	case "dict":
		dict := map[string]any{}

		for {
			key, err := plistValue(dec)
			if errors.Is(err, errEnd) {
				return dict, nil
			}

			if err != nil {
				return nil, err
			}

			value, err := plistValue(dec)
			if err != nil {
				return nil, err
			}

			name, _ := key.(string)
			dict[name] = value
		}
	case "array":
		var list []any

		for {
			value, err := plistValue(dec)
			if errors.Is(err, errEnd) {
				return list, nil
			}

			if err != nil {
				return nil, err
			}

			list = append(list, value)
		}
	case "true", "false":
		return start.Name.Local, dec.Skip()
	default:
		var text string
		if err := dec.DecodeElement(&text, &start); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}

		return strings.TrimSpace(text), nil
	}
}
