// Package shape checks that an answer from an outside source, such as a
// registry or the Homebrew API, holds what oku needs. A source that changes
// its format decodes into empty fields, and oku must not act on half an
// answer.
package shape

import (
	"errors"
	"fmt"
	"strings"
)

// ErrChanged reports an answer that lacks fields oku needs.
var ErrChanged = errors.New("its format may have changed. Check for a newer oku")

// Field is one part of an answer, and whether the answer has it.
type Field struct {
	Name string
	Has  bool
}

// Check returns an error that names source and each field its answer lacks, or
// nil when it has them all.
func Check(source string, fields ...Field) error {
	var missing []string

	for _, f := range fields {
		if !f.Has {
			missing = append(missing, f.Name)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	return fmt.Errorf("%s lacks %s, which oku needs, and %w", source, strings.Join(missing, " and "), ErrChanged)
}
