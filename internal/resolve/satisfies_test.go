package resolve_test

import (
	"testing"

	"github.com/y3owk1n/oku/internal/resolve"
)

func TestB265CaretAndTildeFollowNPM(t *testing.T) {
	for _, c := range []struct {
		version, constraint string
		want                bool
	}{
		{"1.9.3", "^1.4", true},
		{"2.0.0", "^1.4", false},
		{"1.3.9", "^1.4", false},
		{"0.4.7", "^0.4", true},
		{"0.5.0", "^0.4", false},
		{"0.0.3", "^0.0.3", true},
		{"0.0.4", "^0.0.3", false},
		{"1.4.9", "~1.4", true},
		{"1.5.0", "~1.4", false},
		{"1.9.0", "~1", true},
		{"2.0.0", "~1", false},
		{"1.5.0", "^1.4, <1.5", false},
	} {
		got, err := resolve.Satisfies(c.version, c.constraint)
		if err != nil || got != c.want {
			t.Errorf("Satisfies(%q, %q) = %t, %v, want %t", c.version, c.constraint, got, err, c.want)
		}
	}

	if _, err := resolve.Satisfies("1.0.0", "^1.x"); err == nil {
		t.Error("^1.x was accepted, want an error that asks for numbers")
	}
}
