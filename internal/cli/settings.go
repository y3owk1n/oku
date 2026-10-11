package cli

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"

	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/render"
	"github.com/y3owk1n/oku/internal/settings"
)

// settingsStore returns where settings go on this machine, and the name of the
// list's table that it serves.
func settingsStore(opts Options) (settings.Store, string) {
	if opts.Settings != nil {
		return opts.Settings, "defaults"
	}

	return settings.OS()
}

// resolveSettings turns the settings tables of the merged list into the settings
// of a generation. A table for another OS is skipped, like a package whose when
// does not match.
func resolveSettings(
	opts Options,
	listed []list.Setting,
	vars map[string]string,
	notice io.Writer,
) ([]profile.Setting, error) {
	store, backend := settingsStore(opts)

	mine := slices.ContainsFunc(listed, func(s list.Setting) bool { return s.Backend == backend })
	if mine && store.Unavailable() != "" {
		warn(notice, "[%s] is skipped, because %s", backend, store.Unavailable())

		return nil, nil
	}

	var wanted []profile.Setting

	for _, s := range listed {
		if s.Backend != backend {
			if opts.Settings == nil && list.Backends[s.Backend] == runtime.GOOS {
				return nil, fmt.Errorf("[%s] is not built yet, so oku cannot apply it", s.Backend)
			}

			continue
		}

		origin := fmt.Sprintf("%s.%q.%s", s.Backend, s.Domain, s.Key)

		filled, err := fillSetting(s.Value, vars, origin)
		if err != nil {
			return nil, err
		}

		value, err := store.Encode(filled)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", origin, err)
		}

		wanted = append(wanted, profile.Setting{Domain: s.Domain, Key: s.Key, Value: value})
	}

	return wanted, nil
}

// fillSetting fills the locations and [vars] in vars into every string of a
// setting's value, in its arrays and tables too. A generation stores a
// setting's value in plain text, so the value may not name a secret or a
// package.
func fillSetting(value any, vars map[string]string, origin string) (any, error) {
	switch v := value.(type) {
	case string:
		return render.Fill(v, origin, func(name string) (string, error) {
			if reservedVar(name) {
				return "", fmt.Errorf("a setting takes locations and [vars], and %s is neither", name)
			}

			value, set := vars[name]
			if !set {
				return "", fmt.Errorf("%s is not set in [vars]", name)
			}

			return value, nil
		})
	case []any:
		filled := make([]any, len(v))

		for i, item := range v {
			one, err := fillSetting(item, vars, origin)
			if err != nil {
				return nil, err
			}

			filled[i] = one
		}

		return filled, nil
	case map[string]any:
		filled := make(map[string]any, len(v))

		for key, item := range v {
			one, err := fillSetting(item, vars, origin)
			if err != nil {
				return nil, err
			}

			filled[key] = one
		}

		return filled, nil
	}

	return value, nil
}

// settingHandler writes a setting and puts back the value it had before.
func settingHandler(store settings.Store) expose.Handler {
	missing := func(expose.Item) error {
		return errors.New("this OS has no settings that oku can write")
	}

	if store == nil {
		return expose.Handler{Place: missing, Remove: missing}
	}

	return expose.Handler{
		Before: func(item expose.Item) (expose.Item, error) {
			var err error

			item.Prior, item.HadPrior, err = store.Read(item.Domain, item.Key)

			return item, err
		},
		Place: func(item expose.Item) error {
			return store.Write(item.Domain, item.Key, item.Source)
		},
		Remove: func(item expose.Item) error {
			if item.HadPrior {
				return store.Write(item.Domain, item.Key, item.Prior)
			}

			return store.Delete(item.Domain, item.Key)
		},
	}
}

// tellSettings runs the store's Applied with the domains in which before and
// after differ in a setting.
func tellSettings(opts Options, before, after []expose.Item) {
	var domains []string

	for _, pair := range [][2][]expose.Item{{before, after}, {after, before}} {
		for _, item := range pair[0] {
			if item.Kind == "setting" && !slices.Contains(pair[1], item) &&
				!slices.Contains(domains, item.Domain) {
				domains = append(domains, item.Domain)
			}
		}
	}

	if store, _ := settingsStore(opts); store != nil && len(domains) > 0 {
		slices.Sort(domains)
		store.Applied(domains)
	}
}
