package cli

import (
	"slices"

	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/ui"
)

// fileChanges says which files came, went, or got other bytes or another link
// between two generations. On a terminal the home directory reads as "~".
func fileChanges(s ui.Style, from, to []profile.File) []string {
	find := func(files []profile.File, target string) int {
		return slices.IndexFunc(files, func(f profile.File) bool { return f.Target == target })
	}

	var parts []string

	for _, f := range from {
		if find(to, f.Target) < 0 {
			parts = append(parts, s.Bad("-")+" "+s.Home(f.Target))
		}
	}

	for _, f := range to {
		i := find(from, f.Target)

		switch {
		case i < 0:
			parts = append(parts, s.Good("+")+" "+s.Home(f.Target))
		case from[i].Link != f.Link || from[i].Hash != f.Hash || from[i].Mode != f.Mode:
			parts = append(parts, s.Warn("~")+" "+s.Home(f.Target))
		}
	}

	return parts
}

// settingChanges says which settings came, went or changed value between two
// generations, as "domain key".
func settingChanges(s ui.Style, from, to []profile.Setting) []string {
	find := func(settings []profile.Setting, want profile.Setting) int {
		return slices.IndexFunc(settings, func(have profile.Setting) bool {
			return have.Domain == want.Domain && have.Key == want.Key
		})
	}

	var parts []string

	for _, setting := range from {
		if find(to, setting) < 0 {
			parts = append(parts, s.Bad("-")+" "+setting.Domain+" "+setting.Key)
		}
	}

	for _, setting := range to {
		i := find(from, setting)

		switch {
		case i < 0:
			parts = append(parts, s.Good("+")+" "+setting.Domain+" "+setting.Key)
		case from[i].Value != setting.Value:
			parts = append(parts, s.Warn("~")+" "+setting.Domain+" "+setting.Key)
		}
	}

	return parts
}
