package settings

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// EncodeDconf turns a value of the list into GVariant text, which is what the
// dconf tool writes and prints. That text holds the type, so a value that oku
// read goes back as it was.
func EncodeDconf(value any) (string, error) {
	switch v := value.(type) {
	case string:
		escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`).Replace(v)

		return "'" + escaped + "'", nil
	case bool:
		return strconv.FormatBool(v), nil
	case int64:
		// A bare number is an int32 in GVariant text.
		if v < math.MinInt32 || v > math.MaxInt32 {
			return "int64 " + strconv.FormatInt(v, 10), nil
		}

		return strconv.FormatInt(v, 10), nil
	case float64:
		text := strconv.FormatFloat(v, 'g', -1, 64)
		if !strings.ContainsAny(text, ".e") {
			text += ".0"
		}

		return text, nil
	case []any:
		// An empty array has no type that oku could infer.
		if len(v) == 0 {
			return "", errors.New("an empty array has no type, so dconf cannot hold it")
		}

		parts := make([]string, 0, len(v))

		for _, element := range v {
			if reflect.TypeOf(element) != reflect.TypeOf(v[0]) {
				return "", fmt.Errorf(
					"a dconf array holds values of one type, not %v and %v",
					v[0],
					element,
				)
			}

			part, err := EncodeDconf(element)
			if err != nil {
				return "", err
			}

			parts = append(parts, part)
		}

		return "[" + strings.Join(parts, ", ") + "]", nil
	default:
		return "", fmt.Errorf(
			"dconf holds a string, a boolean, an integer, a float or an array of one of them, not %T",
			value,
		)
	}
}

// DconfPath is the key that the dconf tool takes for key in the directory dir of
// the list, such as "org/gnome/desktop/interface".
func DconfPath(dir, key string) string {
	return "/" + strings.Trim(dir, "/") + "/" + key
}
