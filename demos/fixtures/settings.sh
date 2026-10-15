defaults delete com.example.oku-demo >/dev/null 2>&1
cat > ~/.config/oku/oku.toml <<'T'
[packages]
fd = "github:sharkdp/fd"
T
oku sync >/dev/null 2>&1
cat >> ~/.config/oku/oku.toml <<'T'

[defaults."com.example.oku-demo"]
autohide = true
tilesize = 48

[registry.'HKCU\Control Panel\Keyboard']
KeyboardDelay = "0"

[dconf."org/gnome/desktop/interface"]
color-scheme = "prefer-dark"
T
