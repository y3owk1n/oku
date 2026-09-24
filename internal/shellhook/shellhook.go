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
	// StateSaved holds, as JSON, the value each variable had before the hook set
	// or unset it, null for one that was not set.
	StateSaved = "OKU_HOOK_SAVED"
	// StateAdded holds, as JSON, the entries the hook put in front of each list
	// variable, such as PATH.
	StateAdded = "OKU_HOOK_ADDED"
	// StatePath and StateKeys are the state of an older oku, which the hook reads
	// once and removes.
	StatePath = "OKU_HOOK_PATH"
	StateKeys = "OKU_HOOK_KEYS"
	// Project holds the directory of the project that applies, for a prompt to
	// show. The hook and oku exec set it.
	Project = "OKU_PROJECT"
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

// Hook returns the code a shell's startup file loads. It puts dirs on PATH, the
// first one in front, loads the completions of installed programs and of oku
// itself, and runs "oku env" before each prompt. dirs are oku's own directory
// and the global profile's bin, completions is the profile's share/completions,
// and oku is the path of the binary. The one hook line is the whole shell setup.
func Hook(shell string, dirs []string, completions, oku string) (string, error) {
	code, err := promptHook(shell)
	if err != nil {
		return "", err
	}

	return pathSetup(shell, dirs) + completionSetup(shell, completions, oku) + code, nil
}

// completionSetup loads the completions that packages put under dir, and the
// ones oku prints for itself. Each shell finds them its own way: bash sources
// every file now, zsh autoloads the files in dir once compinit has run, fish
// reads dir on demand, and PowerShell has no package completions.
func completionSetup(shell, dir, oku string) string {
	quote := quoter(shell)

	switch shell {
	case "bash":
		return fmt.Sprintf(`if [ -d %[1]s ]; then
  for _oku_file in %[1]s/*; do [ -r "$_oku_file" ] && . "$_oku_file"; done
  unset _oku_file
fi
eval "$(%[2]s completion bash)"
`, quote(dir+"/bash"), quote(oku))
	case "zsh":
		// compinit reads fpath once, so _oku_complete registers the files by name
		// after it has run, from the hook line or from the first prompt, whichever
		// is later.
		return fmt.Sprintf(`fpath=(%[1]s $fpath)
_oku_complete() {
  [ -z "${_OKU_COMPLETE:-}" ] && (( $+functions[compdef] )) || return 0
  _OKU_COMPLETE=1
  local file
  for file in %[1]s/_*(N); do
    autoload -Uz "${file:t}" && compdef "${file:t}" "${${file:t}#_}"
  done
  source <(%[2]s completion zsh)
}
_oku_complete
`, quote(dir+"/zsh"), quote(oku))
	case "fish":
		return fmt.Sprintf(`if test -d %[1]s; and not contains -- %[1]s $fish_complete_path
    set -g fish_complete_path %[1]s $fish_complete_path
end
%[2]s completion fish | source
`, quote(dir+"/fish"), quote(oku))
	case "pwsh":
		return fmt.Sprintf("& %s completion powershell | Out-String | Invoke-Expression\n", quote(oku))
	}

	return ""
}

// pathSetup removes each of dirs from PATH and puts it in front. A login shell
// that tmux starts inside another rebuilds PATH with the system's directories
// before the inherited ones, so a dir already on PATH can sit behind /usr/bin.
// Loading the hook twice changes nothing.
func pathSetup(shell string, dirs []string) string {
	var b strings.Builder

	quote := quoter(shell)

	// Each dir goes to the front, so the last one added ends up first.
	for _, dir := range slices.Backward(dirs) {
		switch shell {
		case "fish":
			fmt.Fprintf(&b, "set -l _oku_dir %s\n"+
				"while set -l _oku_at (contains -i -- $_oku_dir $PATH); set -e PATH[$_oku_at]; end\n"+
				"set -gx PATH $_oku_dir $PATH\n", quote(dir))
		case "pwsh":
			fmt.Fprintf(&b, "$okuDir = %s\n"+
				"$env:PATH = (@($okuDir) + @($env:PATH -split [IO.Path]::PathSeparator | "+
				"Where-Object { $_ -and $_ -ne $okuDir })) -join [IO.Path]::PathSeparator\n", quote(dir))
		case "zsh":
			fmt.Fprintf(&b, "_oku_dir=%s\n"+
				"path=(\"$_oku_dir\" \"${(@)path:#$_oku_dir}\")\n"+
				"export PATH\n", quote(dir))
		default:
			fmt.Fprintf(&b, "_oku_dir=%s\n"+
				"IFS=: read -ra _oku_parts <<< \"$PATH\"\n"+
				"PATH=\"$_oku_dir\"\n"+
				"for _oku_part in \"${_oku_parts[@]}\"; do\n"+
				"  [ \"$_oku_part\" = \"$_oku_dir\" ] || PATH=\"$PATH:$_oku_part\"\n"+
				"done\n"+
				"export PATH\n"+
				"unset _oku_parts _oku_part\n", quote(dir))
		}
	}

	return b.String()
}

func promptHook(shell string) (string, error) {
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
  _oku_complete
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

// Line returns the line a user adds to the shell's startup file. oku is the path
// of the binary, which the line uses in full because oku is not on PATH before
// the hook has run. The line does nothing when that file is gone, so it is safe
// to leave behind.
func Line(shell, oku string) string {
	switch shell {
	case "fish":
		return fmt.Sprintf(`test -x "%[1]s"; and "%[1]s" hook fish | source`, oku)
	case "pwsh":
		return fmt.Sprintf(
			`if (Test-Path "%[1]s") { Invoke-Expression ((& "%[1]s" hook pwsh) -join [Environment]::NewLine) }`,
			oku,
		)
	}

	return fmt.Sprintf(`[ -x "%[1]s" ] && eval "$("%[1]s" hook %[2]s)"`, oku, shell)
}

// StartupFile names the file that Line goes into, as a user would type it.
func StartupFile(shell string) string {
	switch shell {
	case "bash":
		return "~/.bashrc"
	case "zsh":
		return "~/.zshrc"
	case "fish":
		return "~/.config/fish/config.fish"
	case "pwsh":
		return "the file that $PROFILE names"
	}

	return ""
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
