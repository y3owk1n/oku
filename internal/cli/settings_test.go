package cli_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/settings"
)

// fakeSettings stands in for the preference domains of macOS.
type fakeSettings struct {
	values map[string]string
	// failWrite names the keys whose Write fails.
	failWrite map[string]bool
	// applied holds the domains of each Applied call.
	applied [][]string
}

func (f *fakeSettings) Unavailable() string { return "" }

func (f *fakeSettings) Encode(value any) (string, error) { return settings.EncodePlist(value) }

func (f *fakeSettings) Read(domain, key string) (string, bool, error) {
	value, set := f.values[domain+" "+key]

	return value, set, nil
}

func (f *fakeSettings) Write(domain, key, fragment string) error {
	if f.failWrite[key] {
		return fmt.Errorf("the OS refused %s", key)
	}

	f.values[domain+" "+key] = fragment

	return nil
}

func (f *fakeSettings) Delete(domain, key string) error {
	delete(f.values, domain+" "+key)

	return nil
}

func (f *fakeSettings) Applied(domains []string) { f.applied = append(f.applied, domains) }

func settingsMachine(t *testing.T) (machine, *fakeSettings) {
	t.Helper()

	m := newMachine(t)
	store := &fakeSettings{values: map[string]string{}}
	m.opts.Settings = store

	return m, store
}

func TestB149ASettingGetsTheValueAndTheTypeOfTheList(t *testing.T) {
	m, store := settingsMachine(t)

	m.writeFilesList(t, "[defaults.\"com.apple.dock\"]\n"+
		"autohide = true\ntilesize = 48\nautohide-delay = 0.5\norientation = \"left\"\n"+
		"looks-like-xml = \"<true/>\"\n"+
		"[defaults.\".GlobalPreferences\"]\nAppleLanguages = [\"en-SG\", \"ms-MY\"]\n"+
		"[defaults.\"com.apple.Safari\".NSUserKeyEquivalents]\n\"Show Next Tab\" = \"^l\"\n\"Pin Tab\" = \"@d\"\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	for key, want := range map[string]string{
		"com.apple.dock autohide":           "<true/>",
		"com.apple.dock tilesize":           "<integer>48</integer>",
		"com.apple.dock autohide-delay":     "<real>0.5</real>",
		"com.apple.dock orientation":        "<string>left</string>",
		"com.apple.dock looks-like-xml":     "<string>&lt;true/&gt;</string>",
		".GlobalPreferences AppleLanguages": "<array><string>en-SG</string><string>ms-MY</string></array>",
		"com.apple.Safari NSUserKeyEquivalents": "<dict><key>Pin Tab</key><string>@d</string>" +
			"<key>Show Next Tab</key><string>^l</string></dict>",
	} {
		if got := store.values[key]; got != want {
			t.Errorf("%s is %q, want %q", key, got, want)
		}
	}

	want := []string{".GlobalPreferences", "com.apple.Safari", "com.apple.dock"}
	if len(store.applied) != 1 || !slices.Equal(store.applied[0], want) {
		t.Fatalf("oku should tell the OS once which domains changed, got %v", store.applied)
	}

	_, err = m.run(t, "", "sync")
	must(t, err)

	if len(store.applied) != 1 {
		t.Fatalf("a sync that changes no setting should not tell the OS, got %v", store.applied)
	}
}

func TestB150ASettingThatLeavesTheListGetsItsOldValueBack(t *testing.T) {
	m, store := settingsMachine(t)
	store.values["com.apple.dock tilesize"] = "<integer>64</integer>"

	m.writeFilesList(t, "[defaults.\"com.apple.dock\"]\ntilesize = 48\nautohide = true\n")
	_, err := m.run(t, "", "sync")
	must(t, err)

	m.writeFilesList(t, "[defaults.\"com.apple.dock\"]\ntilesize = 32\nautohide = true\n")
	_, err = m.run(t, "", "sync")
	must(t, err)

	if got := store.values["com.apple.dock tilesize"]; got != "<integer>32</integer>" {
		t.Fatalf("a changed value did not arrive, tilesize is %q", got)
	}

	_, err = m.run(t, "", "rollback")
	must(t, err)

	if got := store.values["com.apple.dock tilesize"]; got != "<integer>48</integer>" {
		t.Fatalf("rollback should give the value of generation 1, tilesize is %q", got)
	}

	m.writeFilesList(t, "")
	_, err = m.run(t, "", "sync")
	must(t, err)

	if got := store.values["com.apple.dock tilesize"]; got != "<integer>64</integer>" {
		t.Fatalf("tilesize should be back at the value from before oku, it is %q", got)
	}

	if _, set := store.values["com.apple.dock autohide"]; set {
		t.Fatal("a setting that was not set before oku should be deleted again")
	}
}

func TestB151TheSettingsTablesOfAnotherOSAreSkipped(t *testing.T) {
	m, store := settingsMachine(t)

	m.writeFilesList(t, "[registry.'HKCU\\Control Panel\\Keyboard']\nKeyboardDelay = \"0\"\n"+
		"[dconf.\"org/gnome/desktop/interface\"]\ncolor-scheme = \"prefer-dark\"\n"+
		"[defaults.\"com.apple.dock\"]\ntilesize = 48\n")

	_, err := m.run(t, "", "sync")
	must(t, err)

	if len(store.values) != 1 || store.values["com.apple.dock tilesize"] == "" {
		t.Fatalf("only the table of this OS should be applied, got %v", store.values)
	}
}

func TestB152ARegistryKeyOutsideHKCUIsAnError(t *testing.T) {
	m, _ := settingsMachine(t)

	m.writeFilesList(t, "[registry.'HKLM\\SOFTWARE\\Policies']\nx = \"1\"\n")

	if _, err := m.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), "HKCU") {
		t.Fatalf("a key outside HKCU should be refused, got %v", err)
	}
}

func TestB130AFailedSettingLeavesTheOthersAsTheyWere(t *testing.T) {
	m, store := settingsMachine(t)
	store.values["d aaa"] = "<integer>1</integer>"
	store.failWrite = map[string]bool{"zzz": true}

	m.writeFilesList(t, "[defaults.d]\naaa = 2\nzzz = 3\n")

	_, err := m.run(t, "", "sync")
	if err == nil || !strings.Contains(err.Error(), "the OS refused zzz") {
		t.Fatalf("sync should report the setting that failed, got %v", err)
	}

	if got := store.values["d aaa"]; got != "<integer>1</integer>" {
		t.Fatalf("the setting that sync wrote first should be back at its old value, it is %q", got)
	}
}

func TestB95UninstallRestoresEverySetting(t *testing.T) {
	m, store := settingsMachine(t)
	store.values["com.apple.dock tilesize"] = "<integer>64</integer>"

	m.writeFilesList(t, "[defaults.\"com.apple.dock\"]\ntilesize = 48\nautohide = true\n")
	_, err := m.run(t, "", "sync")
	must(t, err)

	_, err = m.run(t, "", "self", "uninstall", "--yes")
	must(t, err)

	if got := store.values["com.apple.dock tilesize"]; got != "<integer>64</integer>" ||
		len(store.values) != 1 {
		t.Fatalf(
			"uninstall should leave the settings as they were before oku, got %v",
			store.values,
		)
	}
}

func TestB168ASettingOfThisMacGoesToItsOwnDomain(t *testing.T) {
	m, store := settingsMachine(t)
	store.values["currentHost:com.apple.controlcenter BatteryShowPercentage"] = "<false/>"

	m.writeFilesList(
		t,
		"[defaults-currenthost.\"com.apple.controlcenter\"]\nBatteryShowPercentage = true\n"+
			"[defaults.\"com.apple.controlcenter\"]\nOther = 1\n",
	)

	_, err := m.run(t, "", "sync")
	must(t, err)

	if got := store.values["currentHost:com.apple.controlcenter BatteryShowPercentage"]; got != "<true/>" {
		t.Fatalf("the setting of this Mac is %q", got)
	}

	if _, set := store.values["com.apple.controlcenter BatteryShowPercentage"]; set {
		t.Fatal("a setting of this Mac reached the domain of the user on every Mac")
	}

	m.writeFilesList(t, "")

	_, err = m.run(t, "", "sync")
	must(t, err)

	if got := store.values["currentHost:com.apple.controlcenter BatteryShowPercentage"]; got != "<false/>" {
		t.Fatalf("the setting of this Mac should be back at its old value, it is %q", got)
	}
}
