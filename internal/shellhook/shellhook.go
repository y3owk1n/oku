// Package shellhook writes the shell code that applies a project's environment
// when the user enters its directory, and undoes it when they leave.
package shellhook

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Shells are the shells oku can write a hook for.
var Shells = []string{"bash", "zsh", "fish", "pwsh"}

// State variables. The hook keeps what it applied in the environment, so the
// next run can undo exactly that.
const (
	// StatePath holds the bin directory the hook put on PATH.
	StatePath = "OKU_HOOK_PATH"
	// StateKeys holds the names of the variables the hook exported, separated by
	// ":".
	StateKeys = "OKU_HOOK_KEYS"
	// StateHint holds the last hint the hook printed, so a hint appears once per
	// directory and not before every prompt.
	StateHint = "OKU_HOOK_HINT"
)

// Change is what one run of "oku env" asks the shell to do.
type Change struct {
	// Path is the new value of PATH, or empty when PATH stays.
	Path string
	// Set holds variables to export.
	Set map[string]string
	// Unset holds variables to remove.
	Unset []string
	// Hint is a line to show the user, or empty.
	Hint string
}

// Render writes change as commands for shell.
func Render(shell string, change Change) (string, error) {
	if !slices.Contains(Shells, shell) {
		return "", fmt.Errorf("shell %q must be one of %s", shell, strings.Join(Shells, ", "))
	}

	var b strings.Builder

	quote := quoter(shell)
	export, unset, hint := "export %s=%s\n", "unset %s\n", "echo %s >&2\n"

	switch shell {
	case "fish":
		export, unset = "set -gx %s %s\n", "set -e %s\n"
	case "pwsh":
		export, unset = "$env:%s = %s\n", "Remove-Item Env:%s -ErrorAction SilentlyContinue\n"
		hint = "[Console]::Error.WriteLine(%s)\n"
	}

	for _, name := range change.Unset {
		fmt.Fprintf(&b, unset, name)
	}

	if change.Path != "" {
		value := quote(change.Path)
		if shell == "fish" {
			// fish keeps PATH as a list.
			parts := strings.Split(change.Path, ":")
			for i, part := range parts {
				parts[i] = quote(part)
			}

			value = strings.Join(parts, " ")
		}

		fmt.Fprintf(&b, export, "PATH", value)
	}

	for _, name := range slices.Sorted(maps.Keys(change.Set)) {
		fmt.Fprintf(&b, export, name, quote(change.Set[name]))
	}

	if change.Hint != "" {
		fmt.Fprintf(&b, hint, quote(change.Hint))
	}

	return b.String(), nil
}

// Hook returns the code a shell's startup file loads. It runs "oku env" before
// each prompt, and does nothing when oku is not installed.
func Hook(shell string) (string, error) {
	switch shell {
	case "bash":
		return `_oku_hook() {
  local status=$?
  command -v oku >/dev/null 2>&1 && eval "$(oku env --shell bash)"
  return $status
}
case ";${PROMPT_COMMAND:-};" in
  *";_oku_hook;"*) ;;
  *) PROMPT_COMMAND="_oku_hook${PROMPT_COMMAND:+;$PROMPT_COMMAND}" ;;
esac
`, nil
	case "zsh":
		return `_oku_hook() {
  command -v oku >/dev/null 2>&1 && eval "$(oku env --shell zsh)"
}
typeset -ag precmd_functions
if (( ! ${precmd_functions[(I)_oku_hook]} )); then
  precmd_functions=(_oku_hook $precmd_functions)
fi
`, nil
	case "fish":
		return `function _oku_hook --on-event fish_prompt
    command -q oku; and oku env --shell fish | source
end
`, nil
	case "pwsh":
		// The wrapped prompt keeps $LASTEXITCODE, which "oku env" would overwrite.
		return `if (-not (Test-Path Function:\_oku_prompt)) {
  Copy-Item Function:\prompt Function:\_oku_prompt
  function global:prompt {
    $okuLast = $global:LASTEXITCODE
    if (Get-Command oku -ErrorAction SilentlyContinue) {
      $okuCode = (& oku env --shell pwsh) -join [Environment]::NewLine
      if ($okuCode) { Invoke-Expression $okuCode }
    }
    $global:LASTEXITCODE = $okuLast
    _oku_prompt
  }
}
`, nil
	default:
		return "", fmt.Errorf("shell %q must be one of %s", shell, strings.Join(Shells, ", "))
	}
}

// Line returns the line a user adds to the shell's startup file. It does nothing
// when oku is not installed, so it is safe to leave behind.
func Line(shell string) string {
	switch shell {
	case "fish":
		return "command -q oku; and oku hook fish | source"
	case "pwsh":
		return "if (Get-Command oku -ErrorAction SilentlyContinue) " +
			"{ Invoke-Expression ((& oku hook pwsh) -join [Environment]::NewLine) }"
	}

	return fmt.Sprintf(`command -v oku >/dev/null 2>&1 && eval "$(oku hook %s)"`, shell)
}

// quoter returns the function that wraps a string in single quotes for shell.
// fish escapes a quote inside them with a backslash. bash and zsh cannot, so the
// string is closed, an escaped quote added, and reopened.
func quoter(shell string) func(string) string {
	switch shell {
	case "fish":
		return func(s string) string {
			return "'" + strings.NewReplacer(`\`, `\\`, "'", `\'`).Replace(s) + "'"
		}
	case "pwsh":
		// PowerShell doubles a quote inside single quotes.
		return func(s string) string {
			return "'" + strings.ReplaceAll(s, "'", "''") + "'"
		}
	}

	return func(s string) string {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
}
