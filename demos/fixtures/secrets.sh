C=~/.config/oku
mkdir -p $C/secrets $C/files ~/.config/sops/age
age-keygen -o ~/.config/sops/age/keys.txt 2>/dev/null
printf 'creation_rules:\n  - path_regex: secrets/.*\n    age: %s\n' "$(age-keygen -y ~/.config/sops/age/keys.txt)" > $C/.sops.yaml
printf 'github:\n  token: ghp_demo_not_a_real_token\n' > $C/secrets/secrets.yaml
(cd $C && SOPS_AGE_KEY_FILE=~/.config/sops/age/keys.txt sops encrypt --in-place secrets/secrets.yaml)
printf 'github.com:\n    oauth_token: {{secret.github_token}}\n' > $C/files/gh-hosts.tmpl
cat > $C/oku.toml <<'T'
[secrets]
github_token = { file = "./secrets/secrets.yaml", key = "github/token" }

[files]
"{{config}}/gh/hosts.yml" = { render = "./files/gh-hosts.tmpl", mode = "0600" }
T
cd $C
