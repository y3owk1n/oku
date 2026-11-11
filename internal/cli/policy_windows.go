package cli

import "golang.org/x/sys/windows/registry"

// blockingScriptPolicy returns the execution policy that Group Policy sets for
// PowerShell, when it is one that keeps PowerShell from loading an unsigned
// profile: Restricted or AllSigned. It is "" otherwise. Windows PowerShell and
// PowerShell 7 each read a key of their own, and in each the machine's policy
// comes before the user's.
func blockingScriptPolicy() string {
	for _, path := range []string{
		`SOFTWARE\Policies\Microsoft\Windows\PowerShell`, `SOFTWARE\Policies\Microsoft\PowerShellCore`,
	} {
		for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
			policy, set := groupPolicy(root, path)
			if !set {
				continue
			}

			if policy == "Restricted" || policy == "AllSigned" {
				return policy
			}

			break
		}
	}

	return ""
}

// groupPolicy reads the execution policy under root\path. EnableScripts 0
// turns scripts off, which PowerShell reports as Restricted. set is false when
// the key does not turn the policy on or off.
func groupPolicy(root registry.Key, path string) (policy string, set bool) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer func() { _ = key.Close() }()

	enabled, _, err := key.GetIntegerValue("EnableScripts")
	if err != nil {
		return "", false
	}

	if enabled == 0 {
		return "Restricted", true
	}

	policy, _, err = key.GetStringValue("ExecutionPolicy")
	if err != nil {
		return "", false
	}

	return policy, true
}
