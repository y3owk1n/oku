package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/ui"
)

func newListCmd(opts Options) *cobra.Command {
	var files, settings bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed packages, or the files and settings of the list",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := scopedEnv(cmd, opts)
			if err != nil {
				return err
			}

			switch {
			case files && settings:
				return errors.New("pass --files or --settings, not both")
			case files:
				return e.listFiles(cmd, opts)
			case settings:
				return e.listSettings(cmd, opts)
			}

			pkgs, err := e.profile().Packages()
			if err != nil {
				return err
			}

			if wantJSON(cmd) {
				type row struct {
					Name      string `json:"name"`
					Version   string `json:"version"`
					Ref       string `json:"ref"`
					StorePath string `json:"store_path"`
					Service   bool   `json:"service"`
					System    bool   `json:"system"`
				}

				rows := []row{}
				for _, pkg := range pkgs {
					rows = append(rows, row{
						pkg.Name, pkg.Version, pkg.Ref, pkg.StorePath, pkg.Service, pkg.System,
					})
				}

				return printJSON(cmd, rows)
			}

			if len(pkgs) == 0 {
				fmt.Fprintln(
					cmd.OutOrStdout(),
					"no packages installed, `oku add <ref>` installs one",
				)

				return nil
			}

			out := cmd.OutOrStdout()
			s := ui.For(out)
			tab := s.Table("name", "version", "ref", "")

			for _, pkg := range pkgs {
				cells := []string{pkg.Name, pkg.Version, s.Home(pkg.Ref)}

				// A terminal gets a column that says what else the package does.
				if s.On() {
					var marks []string
					if pkg.Service {
						marks = append(marks, "service")
					}

					if pkg.System {
						marks = append(marks, "system")
					}

					cells = append(cells, strings.Join(marks, " "))
				}

				tab.Styled(cells, s.Bold, nil, s.Dim, s.Accent)
			}

			if err := tab.Write(out); err != nil {
				return err
			}

			// A terminal gets a footer with the count and which list this is.
			if s.On() {
				where := "the global list"
				if e.project != "" {
					where = "the project " + s.Home(e.project)
				}

				fmt.Fprintln(out, s.Dim(count(len(pkgs), "package")+" in "+where))
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&files, "files", false, "list the files of the list and where each comes from")
	cmd.Flags().
		BoolVar(&settings, "settings", false, "list the settings of the list with the value each had before oku")

	return cmd
}

// listFiles prints the [files] entries that apply to this machine: the target,
// what kind of entry it is, its source and the list that declares it.
func (e env) listFiles(cmd *cobra.Command, opts Options) error {
	all, err := e.mergedList(cmd, opts)
	if err != nil {
		return err
	}

	type row struct {
		Target string `json:"target"`
		Kind   string `json:"kind"`
		Source string `json:"source,omitempty"`
		List   string `json:"list"`
	}

	host := platform.Host()
	rows := []row{}

	for _, f := range all.files {
		if !f.file.When.Matches(host) {
			continue
		}

		kind, source := "text", ""

		switch {
		case f.file.Link != "":
			kind, source = "link", f.file.Link
		case f.file.Render != "":
			kind, source = "render", f.file.Render
		case f.file.Secret != "":
			kind, source = "secret", f.file.Secret
		}

		rows = append(rows, row{f.file.Target, kind, source, f.dir})
	}

	if wantJSON(cmd) {
		return printJSON(cmd, rows)
	}

	out := cmd.OutOrStdout()

	if len(rows) == 0 {
		fmt.Fprintln(out, "the list has no files for this machine, a [files] table in oku.toml adds one")

		return nil
	}

	s := ui.For(out)
	tab := s.Table("target", "kind", "source", "list")

	for _, r := range rows {
		tab.Styled([]string{r.Target, r.Kind, s.Home(r.Source), s.Home(r.List)}, s.Bold, nil, nil, s.Dim)
	}

	return tab.Write(out)
}

// listSettings prints the settings of this machine's backend: the domain, the
// key, the value the list wants and the value the key had before oku wrote it.
func (e env) listSettings(cmd *cobra.Command, opts Options) error {
	all, err := e.mergedList(cmd, opts)
	if err != nil {
		return err
	}

	ledger, err := expose.ReadLedger(e.data)
	if err != nil {
		return err
	}

	_, backend := settingsStore(opts)

	type row struct {
		Domain   string `json:"domain"`
		Key      string `json:"key"`
		Value    any    `json:"value"`
		Prior    string `json:"prior,omitempty"`
		HadPrior bool   `json:"had_prior"`
	}

	rows := []row{}

	for _, setting := range all.settings {
		if setting.Backend != backend {
			continue
		}

		r := row{Domain: setting.Domain, Key: setting.Key, Value: setting.Value}

		for _, item := range ledger.Items {
			if item.Kind == "setting" && item.Domain == setting.Domain && item.Key == setting.Key {
				r.Prior, r.HadPrior = item.Prior, item.HadPrior
			}
		}

		rows = append(rows, r)
	}

	if wantJSON(cmd) {
		return printJSON(cmd, rows)
	}

	out := cmd.OutOrStdout()

	if len(rows) == 0 {
		fmt.Fprintf(out, "the list has no settings, a [%s] table in oku.toml adds one\n", backend)

		return nil
	}

	s := ui.For(out)
	tab := s.Table("domain", "key", "value", "before oku")

	for _, r := range rows {
		// A prior value is the fragment the OS gave back, which may span lines.
		prior := "not set"
		if r.HadPrior {
			prior = strings.Join(strings.Fields(r.Prior), " ")
		}

		value, err := json.Marshal(r.Value)
		if err != nil {
			return err
		}

		tab.Styled([]string{r.Domain, r.Key, string(value), prior}, s.Bold, nil, nil, s.Dim)
	}

	return tab.Write(out)
}

// mergedList reads the list with its includes at the commits oku.lock pins.
func (e env) mergedList(cmd *cobra.Command, opts Options) (merged, error) {
	locked, err := lock.Read(e.lockPath())
	if err != nil {
		return merged{}, err
	}

	return e.loadList(cmd.Context(), opts, locked, false)
}
