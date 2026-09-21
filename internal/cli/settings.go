package cli

import (
	"errors"
	"fmt"
	"runtime"
	"slices"

	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/profile"
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
func resolveSettings(opts Options, listed []list.Setting) ([]profile.Setting, error) {
	store, backend := settingsStore(opts)

	var wanted []profile.Setting

	for _, s := range listed {
		if s.Backend != backend {
			if opts.Settings == nil && list.Backends[s.Backend] == runtime.GOOS {
				return nil, fmt.Errorf("[%s] is not built yet, so oku cannot apply it", s.Backend)
			}

			continue
		}

		value, err := store.Encode(s.Value)
		if err != nil {
			return nil, fmt.Errorf("%s.%q.%s: %w", s.Backend, s.Domain, s.Key, err)
		}

		wanted = append(wanted, profile.Setting{Domain: s.Domain, Key: s.Key, Value: value})
	}

	return wanted, nil
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
