package settings

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// EncodeRegistry turns a value of the list into a registry fragment, which is
// the type, a colon and the data the way reg.exe writes and prints them.
func EncodeRegistry(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return "REG_SZ:" + v, nil
	case bool:
		if v {
			return "REG_DWORD:1", nil
		}

		return "REG_DWORD:0", nil
	case int64:
		switch {
		case v < 0:
			return "", fmt.Errorf("%d is negative, and a registry number is not", v)
		case v <= math.MaxUint32:
			return "REG_DWORD:" + strconv.FormatInt(v, 10), nil
		default:
			return "REG_QWORD:" + strconv.FormatInt(v, 10), nil
		}
	case []any:
		parts := make([]string, 0, len(v))

		for _, element := range v {
			text, ok := element.(string)
			if !ok || text == "" {
				return "", fmt.Errorf(
					"a registry array holds strings that are not empty, not %v",
					element,
				)
			}

			parts = append(parts, text)
		}

		// reg.exe separates the strings of a REG_MULTI_SZ with a backslash and a zero.
		return "REG_MULTI_SZ:" + strings.Join(parts, `\0`), nil
	default:
		return "", fmt.Errorf(
			"the registry holds a string, a boolean, an integer or an array of strings, not %T",
			value,
		)
	}
}

// FindRegistry returns the fragment of the value called name in the output of
// "reg query <key> /v <name>", and whether it is there. reg.exe prints the name,
// the type and the data of a value in one line, four spaces apart.
func FindRegistry(output, name string) (string, bool) {
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.SplitN(strings.TrimRight(line, "\r"), "    ", 4)
		if len(fields) < 3 || fields[0] != "" || fields[1] != name ||
			!strings.HasPrefix(fields[2], "REG_") {
			continue
		}

		data := ""
		if len(fields) == 4 {
			data = fields[3]
		}

		return fields[2] + ":" + data, true
	}

	return "", false
}
