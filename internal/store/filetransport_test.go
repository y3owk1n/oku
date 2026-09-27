package store

import (
	"net/url"
	"testing"
)

func TestB431AFileURLNamesTheFileOfEachOS(t *testing.T) {
	for _, tc := range []struct{ url, goos, want string }{
		{"file:///C:/tools/x.zip", "windows", `C:\tools\x.zip`},
		{"file:///c:/a%20b/x.zip", "windows", `c:\a b\x.zip`},
		{"file://server/share/x.zip", "windows", `\\server\share\x.zip`},
		{"file://localhost/C:/x.zip", "windows", `C:\x.zip`},
		{"file:///home/me/x.zip", "linux", "/home/me/x.zip"},
		{"file://localhost/home/me/x.zip", "darwin", "/home/me/x.zip"},
	} {
		u, err := url.Parse(tc.url)
		if err != nil {
			t.Fatal(err)
		}

		if got, err := localPath(u, tc.goos); err != nil || got != tc.want {
			t.Errorf("%s on %s is %q, %v, want %q", tc.url, tc.goos, got, err, tc.want)
		}
	}

	u, _ := url.Parse("file://server/share/x.zip")
	if _, err := localPath(u, "linux"); err == nil {
		t.Error("a file of another machine should fail outside Windows")
	}
}
