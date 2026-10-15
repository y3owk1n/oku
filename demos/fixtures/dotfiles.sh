C=~/.config/oku
mkdir -p $C/files
printf 'set number\nset relativenumber\n' > $C/files/vimrc
printf 'font-family = {{font}}\ntheme = {{theme}}\n' > $C/files/ghostty.tmpl
cat > $C/oku.toml <<'T'
[vars]
font = "JetBrainsMono Nerd Font"
theme = "catppuccin-mocha"

[files]
"{{home}}/.vimrc" = { link = "./files/vimrc" }
"{{home}}/.config/ghostty/config" = { render = "./files/ghostty.tmpl" }
T
cd $C
