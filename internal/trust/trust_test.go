package trust_test

import (
	"testing"

	"github.com/y3owk1n/oku/internal/trust"
)

func TestB545ASaveKeepsWhatAnotherOkuAllowedMeanwhile(t *testing.T) {
	data := t.TempDir()

	// The hook read the file before another oku allowed a second project.
	hook, err := trust.ReadAllowed(data)
	if err != nil {
		t.Fatal(err)
	}

	other, err := trust.ReadAllowed(data)
	if err != nil {
		t.Fatal(err)
	}

	if err := other.Set("/work/b", &trust.Allow{ListSHA256: "b"}); err != nil {
		t.Fatal(err)
	}

	if err := hook.Set("/work/a", &trust.Allow{ListSHA256: "a"}); err != nil {
		t.Fatal(err)
	}

	now, err := trust.ReadAllowed(data)
	if err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{"/work/a", "/work/b"} {
		if _, ok := now.Get(dir); !ok {
			t.Fatalf("the allow of %s is gone: %+v", dir, now.Items)
		}
	}
}
