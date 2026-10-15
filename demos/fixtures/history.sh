oku add github:BurntSushi/ripgrep@14.1.1 >/dev/null 2>&1
sed -i '' 's|^ripgrep = .*|ripgrep = "github:BurntSushi/ripgrep"|' ~/.config/oku/oku.toml
