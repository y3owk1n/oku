package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/platform"
)

// schema is every key a manifest may hold, including the parts oku does not act
// on yet. Lint decodes into it strictly, so a misspelt key is an error even in a
// section that Parse ignores.
type schema struct {
	Package struct {
		Name        string `toml:"name"`
		Description string `toml:"description"`
		Homepage    string `toml:"homepage"`
		License     string `toml:"license"`
		Relocatable bool   `toml:"relocatable"`
		SigningKey  string `toml:"signing_key"`
	} `toml:"package"`
	Version   Version `toml:"version"`
	Artifacts []struct {
		Match        platform.Selector `toml:"match"`
		URL          string            `toml:"url"`
		SHA256       string            `toml:"sha256"`
		SHA256URL    string            `toml:"sha256_url"`
		SignatureURL string            `toml:"signature_url"`
		Strip        int               `toml:"strip"`
		Bin          []string          `toml:"bin"`
		Lib          []string          `toml:"lib"`
		Include      []string          `toml:"include"`
		Man          []string          `toml:"man"`
		Completions  map[string]string `toml:"completions"`
		Share        []string          `toml:"share"`
		App          []string          `toml:"app"`
		Font         []string          `toml:"font"`
	} `toml:"artifact"`
	Build    Build             `toml:"build"`
	Runtime  Runtime           `toml:"runtime"`
	Env      map[string]string `toml:"env"`
	Apps     []App             `toml:"app"`
	Services []Service         `toml:"service"`
}

// VendorKinds are the values a vendor step accepts. The store holds how each
// one runs.
var VendorKinds = []string{"cargo", "go", "npm", "pip"}

var (
	artifactVars = []string{"version", "tag", "os", "arch", "libc"}
	buildVars    = append([]string{"prefix", "src", "jobs"}, artifactVars...)
)

// Report is what Lint found. A manifest with Errors must not be published.
type Report struct {
	Errors   []string
	Warnings []string
}

// Lint checks manifest data against the whole schema.
func Lint(data []byte) Report {
	var (
		report Report
		full   schema
	)

	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	err := decoder.Decode(&full)

	var strict *toml.StrictMissingError

	switch {
	case errors.As(err, &strict):
		for _, missing := range strict.Errors {
			row, _ := missing.Position()
			report.Errors = append(report.Errors, fmt.Sprintf(
				"line %d: unknown key %s", row, strings.Join(missing.Key(), "."),
			))
		}
	case err != nil:
		report.Errors = append(report.Errors, err.Error())

		return report
	}

	var m Manifest
	if err := toml.Unmarshal(data, &m); err == nil {
		if err := m.validate(); err != nil {
			report.Errors = append(report.Errors, strings.Split(err.Error(), "\n")...)
		}
	}

	for i, a := range full.Artifacts {
		for _, text := range []string{a.URL, a.SHA256URL} {
			for _, name := range unknownVars(text, artifactVars) {
				report.Errors = append(report.Errors, fmt.Sprintf(
					"artifact[%d]: unknown template variable {{%s}}", i, name,
				))
			}
		}

		if a.SHA256 == "" && a.SHA256URL == "" {
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"artifact[%d]: no sha256 or sha256_url, so users trust the first download", i,
			))
		}
	}

	for i, s := range full.Build.Steps {
		report.Errors = append(report.Errors, lintStep(i, s)...)
	}

	if full.Package.Description == "" {
		report.Warnings = append(
			report.Warnings,
			"package.description is empty, and `oku search` matches it",
		)
	}

	return report
}

func lintStep(i int, s Step) []string {
	var found []string

	if s.Run != nil {
		// A step with no when.os, or with when.os = "windows", can run on Windows,
		// where no default shell exists.
		if (s.When.OS == "" || s.When.OS == "windows") && s.Shell == "" {
			found = append(found, fmt.Sprintf(
				"build.step[%d]: this run step can run on Windows, so set shell, "+
					`or limit it with when = { os = "..." }`, i,
			))
		}

		for _, name := range unknownVars(*s.Run, buildVars) {
			found = append(
				found,
				fmt.Sprintf("build.step[%d]: unknown template variable {{%s}}", i, name),
			)
		}
	}

	if s.Vendor != nil && !slices.Contains(VendorKinds, *s.Vendor) {
		found = append(found, fmt.Sprintf(
			"build.step[%d]: vendor %q must be one of %s",
			i,
			*s.Vendor,
			strings.Join(VendorKinds, ", "),
		))
	}

	if s.Fetch != nil {
		if s.Fetch.SHA256 == "" {
			found = append(found, fmt.Sprintf("build.step[%d]: a fetch step needs sha256", i))
		}
	}

	return found
}

// unknownVars returns the template variables in text that are not in known.
// "dep.<name>.prefix" is always known.
func unknownVars(text string, known []string) []string {
	var unknown []string

	for _, match := range templateRe.FindAllStringSubmatch(text, -1) {
		name := match[1]
		if slices.Contains(known, name) ||
			strings.HasPrefix(name, "dep.") && strings.HasSuffix(name, ".prefix") {
			continue
		}

		unknown = append(unknown, name)
	}

	return unknown
}
