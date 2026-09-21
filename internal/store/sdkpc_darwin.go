package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// systemLibrary is a library of macOS that the SDK has headers for and no
// pkg-config file. version finds its version in header, under the SDK's
// usr/include.
type systemLibrary struct {
	name, description, header, libs, cflags string
	version                                 *regexp.Regexp
	// fixed is the version of a library whose header does not state one.
	fixed string
}

var systemLibraries = []systemLibrary{
	{
		name: "zlib", description: "zlib compression library", header: "zlib.h", libs: "-lz",
		version: regexp.MustCompile(`#define ZLIB_VERSION "([^"]+)"`),
	},
	{
		name: "expat", description: "expat XML parser", header: "expat.h", libs: "-lexpat",
		version: regexp.MustCompile(
			`XML_MAJOR_VERSION (\d+)[\s\S]*?XML_MINOR_VERSION (\d+)[\s\S]*?XML_MICRO_VERSION (\d+)`,
		),
	},
	{
		name: "libxml-2.0", description: "libXML library version2", libs: "-lxml2",
		header: "libxml2/libxml/xmlversion.h", cflags: "-I${includedir}/libxml2",
		version: regexp.MustCompile(`#define LIBXML_DOTTED_VERSION "([^"]+)"`),
	},
	{
		name: "sqlite3", description: "SQL database engine", header: "sqlite3.h", libs: "-lsqlite3",
		version: regexp.MustCompile(`#define SQLITE_VERSION\s+"([^"]+)"`),
	},
	{
		name: "libcurl", description: "Library to transfer files", header: "curl/curlver.h",
		libs: "-lcurl", version: regexp.MustCompile(`#define LIBCURL_VERSION "([^"]+)"`),
	},
	{
		name: "ncurses", description: "ncurses library", header: "curses.h", libs: "-lncurses",
		version: regexp.MustCompile(`#define NCURSES_VERSION "([^"]+)"`),
	},
	// bzlib.h states no version. macOS has shipped 1.0.8 since 2019, and Homebrew
	// names the same one.
	{name: "bzip2", description: "bzip2 compression", header: "bzlib.h", libs: "-lbz2", fixed: "1.0.8"},
}

// writeSystemPkgConfig writes a pkg-config file into dir for each library of
// macOS that the SDK has a header for. macOS ships none of these files, and a
// dep's own file often says "Requires: zlib", which pkg-config then cannot
// resolve. The version comes from the header of the SDK on this machine. It
// returns an empty string when there is no SDK.
func writeSystemPkgConfig(dir string) string {
	out, err := exec.Command("/usr/bin/xcrun", "--show-sdk-path").Output()
	if err != nil {
		return ""
	}

	include := filepath.Join(strings.TrimSpace(string(out)), "usr", "include")
	wrote := false

	for _, lib := range systemLibraries {
		header, err := os.ReadFile(filepath.Join(include, filepath.FromSlash(lib.header)))
		if err != nil {
			continue
		}

		version := lib.fixed
		if lib.version != nil {
			found := lib.version.FindSubmatch(header)
			if found == nil {
				continue
			}

			parts := make([]string, 0, len(found)-1)
			for _, part := range found[1:] {
				parts = append(parts, string(part))
			}

			version = strings.Join(parts, ".")
		}

		// The linker finds a system library in the SDK by itself, so there is no -L.
		body := fmt.Sprintf(
			"includedir=%s\n\nName: %s\nDescription: %s\nVersion: %s\nLibs: %s\nCflags: %s\n",
			include, lib.name, lib.description, version, lib.libs, lib.cflags,
		)

		if os.MkdirAll(dir, 0o755) != nil ||
			os.WriteFile(filepath.Join(dir, lib.name+".pc"), []byte(body), 0o644) != nil {
			return ""
		}

		wrote = true
	}

	if !wrote {
		return ""
	}

	return dir
}
