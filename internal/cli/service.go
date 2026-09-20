package cli

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/service"
	"github.com/y3owk1n/oku/internal/store"
)

// services returns the OS's service manager, or the one the tests supplied.
func (e env) services(opts Options) (service.Manager, error) {
	if opts.Services != nil {
		return opts.Services, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	return service.New(home, e.data), nil
}

// systemServices returns the manager for services in system scope.
func systemServices(opts Options) service.Manager {
	if opts.Services != nil {
		return opts.Services
	}

	return service.NewSystem()
}

// definition turns a manifest's service into what the manager runs. Paths and
// {{prefix}} resolve inside the package's store path.
func (e env) definition(pkg profile.Package, svc manifest.Service) (service.Definition, error) {
	d := service.Definition{
		Name:    svc.Name,
		Program: filepath.Join(pkg.StorePath, filepath.FromSlash(svc.Command)),
		Restart: svc.Restart,
		Env:     map[string]string{},
		LogFile: filepath.Join(e.data, "logs", svc.Name+".log"),
	}

	if pkg.System {
		d.LogFile = filepath.Join(service.SystemLogDir(), svc.Name+".log")
	}

	vars := map[string]string{"prefix": pkg.StorePath, "version": pkg.Version}

	for _, arg := range svc.Args {
		expanded, err := manifest.Expand(arg, vars)
		if err != nil {
			return d, fmt.Errorf("service %s: %w", svc.Name, err)
		}

		d.Args = append(d.Args, expanded)
	}

	for name, value := range svc.Env {
		expanded, err := manifest.Expand(value, vars)
		if err != nil {
			return d, fmt.Errorf("service %s: %w", svc.Name, err)
		}

		d.Env[name] = expanded
	}

	return d, nil
}

// serviceItems lists the services of pkgs as ledger items, and returns their
// definitions by name for the handler.
func (e env) serviceItems(
	opts Options,
	pkgs []profile.Package,
) ([]expose.Item, map[string]service.Definition, error) {
	user, err := e.services(opts)
	if err != nil {
		return nil, nil, err
	}

	var items []expose.Item

	defs := map[string]service.Definition{}

	for _, pkg := range pkgs {
		meta, err := store.ReadMeta(pkg.StorePath)
		if err != nil {
			continue
		}

		for _, svc := range meta.Services {
			d, err := e.definition(pkg, svc)
			if err != nil {
				return nil, nil, err
			}

			if other, taken := defs[d.Name]; taken {
				return nil, nil, fmt.Errorf(
					"two packages ship a service called %s: %s and %s",
					d.Name,
					other.Program,
					d.Program,
				)
			}

			manager := user
			if pkg.System {
				manager = systemServices(opts)
			}

			defs[d.Name] = d
			items = append(items, expose.Item{
				Kind: "service", Package: pkg.Name, Source: d.Program,
				Target: manager.File(d), Name: d.Name, Enabled: pkg.Service, System: pkg.System,
			})
		}
	}

	return items, defs, nil
}

// serviceHandler installs and removes services through the manager.
func serviceHandler(manager service.Manager, defs map[string]service.Definition) expose.Handler {
	return expose.Handler{
		Place: func(item expose.Item) error {
			return manager.Install(context.Background(), defs[item.Name], item.Enabled)
		},
		Remove: func(item expose.Item) error {
			return manager.Remove(context.Background(), service.Definition{Name: item.Name})
		},
	}
}

func newServiceCmd(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Control the services of installed packages",
		Long: `Control the services of installed packages.

A package's services are installed with it and stay stopped. To run one now and
at every login, set service = true on the package in oku.toml, or install it
with "oku add --service", and run "oku sync".

start and stop act on this login session only.`,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the services of installed packages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listServices(cmd, opts)
		},
	})

	for _, action := range []struct{ name, short string }{
		{"start", "Start a service for this login session"},
		{"stop", "Stop a service"},
		{"restart", "Stop a service and start it again"},
		{"status", "Show whether a service is enabled and running"},
		{"logs", "Show the last lines a service printed"},
	} {
		sub := &cobra.Command{
			Use:   action.name + " <name>",
			Short: action.short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return controlService(cmd, opts, action.name, args[0])
			},
		}

		if action.name != "status" {
			sub.Flags().
				Bool(systemFlag, false, "control a service in system scope, which needs administrator rights")
		}

		cmd.AddCommand(sub)
	}

	return cmd
}

// globalServices returns the services of the global profile's active generation.
func globalServices(opts Options) (env, map[string]service.Definition, []expose.Item, error) {
	e, err := loadEnv()
	if err != nil {
		return e, nil, nil, err
	}

	pkgs, err := e.globalProfile().Packages()
	if err != nil {
		return e, nil, nil, err
	}

	items, defs, err := e.serviceItems(opts, pkgs)

	return e, defs, items, err
}

// managerFor returns the manager that runs item.
func (e env) managerFor(opts Options, item expose.Item) (service.Manager, error) {
	if item.System {
		return systemServices(opts), nil
	}

	return e.services(opts)
}

func listServices(cmd *cobra.Command, opts Options) error {
	e, defs, items, err := globalServices(opts)
	if err != nil {
		return err
	}

	if wantJSON(cmd) {
		rows := []serviceRow{}

		for _, item := range items {
			manager, err := e.managerFor(opts, item)
			if err != nil {
				return err
			}

			status, err := manager.Status(cmd.Context(), defs[item.Name])
			if err != nil {
				return err
			}

			rows = append(rows, newServiceRow(item, status))
		}

		return printJSON(cmd, rows)
	}

	if len(items) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no installed package ships a service")

		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

	for _, item := range items {
		manager, err := e.managerFor(opts, item)
		if err != nil {
			return err
		}

		status, err := manager.Status(cmd.Context(), defs[item.Name])
		if err != nil {
			return err
		}

		fmt.Fprintf(w, "%s\t%s\t%s\n", item.Name, item.Package, describeStatus(status, item.System))
	}

	return w.Flush()
}

// serviceRow is the JSON form of one service.
type serviceRow struct {
	Name      string `json:"name"`
	Package   string `json:"package"`
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Running   bool   `json:"running"`
	System    bool   `json:"system"`
	Detail    string `json:"detail,omitempty"`
}

func newServiceRow(item expose.Item, status service.Status) serviceRow {
	return serviceRow{
		item.Name, item.Package, status.Installed, status.Enabled, status.Running,
		item.System, status.Detail,
	}
}

// rootManager starts and stops a system service through "oku system-apply".
type rootManager struct {
	service.Manager
	run func(action string) error
}

func (m rootManager) Start(context.Context, service.Definition) error { return m.run("start") }
func (m rootManager) Stop(context.Context, service.Definition) error  { return m.run("stop") }

func describeStatus(status service.Status, system bool) string {
	parts := []string{"stopped"}
	if status.Running {
		parts = []string{"running"}
	}

	switch {
	case status.Enabled && system:
		parts = append(parts, "starts at boot")
	case status.Enabled:
		parts = append(parts, "starts at login")
	}

	if system {
		parts = append(parts, "system scope")
	}

	if status.Detail != "" {
		parts = append(parts, status.Detail)
	}

	return strings.Join(parts, ", ")
}

func controlService(cmd *cobra.Command, opts Options, action, name string) error {
	e, defs, items, err := globalServices(opts)
	if err != nil {
		return err
	}

	d, ok := defs[name]
	if !ok {
		known := slices.Sorted(maps.Keys(defs))

		return fmt.Errorf("no installed package ships a service called %s, the services are: %s",
			name, strings.Join(known, ", "))
	}

	ctx, out := cmd.Context(), cmd.OutOrStdout()

	item := items[slices.IndexFunc(items, func(item expose.Item) bool { return item.Name == name })]

	manager, err := e.managerFor(opts, item)
	if err != nil {
		return err
	}

	system, _ := cmd.Flags().GetBool(systemFlag)

	// Reading a system service's log needs root on Linux, where it is in the
	// system journal, so logs takes the flag too and works without it on macOS.
	if item.System && action == "logs" && system {
		return e.applyAsRoot(ctx, opts, "logs", systemChange{item, d})
	}

	if item.System && action != "status" && action != "logs" {
		if !system {
			return fmt.Errorf(
				"%s runs in system scope, so %s needs administrator rights: run \"oku service %s %s --system\"",
				name,
				action,
				action,
				name,
			)
		}

		// Only root can control a system service, so oku runs itself as root.
		manager = rootManager{Manager: manager, run: func(action string) error {
			return e.applyAsRoot(ctx, opts, action, systemChange{item, d})
		}}
	}

	switch action {
	case "start":
		err = manager.Start(ctx, d)
	case "stop":
		err = manager.Stop(ctx, d)
	case "restart":
		if err = manager.Stop(ctx, d); err == nil {
			err = manager.Start(ctx, d)
		}
	case "logs":
		var text string
		if text, err = manager.Logs(ctx, d, 50); err == nil {
			fmt.Fprintln(out, text)
		} else if item.System {
			err = fmt.Errorf(
				"%w\nif that is a permission error, run \"oku service logs %s --system\"",
				err,
				name,
			)
		}

		return err
	}

	if err != nil {
		return err
	}

	status, err := manager.Status(ctx, d)
	if err != nil {
		return err
	}

	if wantJSON(cmd) {
		return printJSON(cmd, newServiceRow(item, status))
	}

	fmt.Fprintf(out, "%s: %s\n", name, describeStatus(status, item.System))

	return nil
}
