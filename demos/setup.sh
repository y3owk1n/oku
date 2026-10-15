# Sourced by each tape in a hidden prelude: `source demos/setup.sh <scene>`.
# Gives the scene a throwaway HOME under /tmp/oku-demo, so a recording never
# touches the real ~/.config/oku and ~/.local/share/oku.

scene=$1
demos=${0:A:h}
root=/tmp/oku-demo
S=$root/$scene

# oku is copied, not linked, so its path on screen is /tmp/oku-demo/bin/oku.
mkdir -p $root/bin
[ -e $root/bin/oku ] || cp "$(command -v oku)" $root/bin/oku
for t in sops age age-keygen bat; do
  [ -e $root/bin/$t ] || ln -s "$(command -v $t)" $root/bin/$t
done

[ -d "$S" ] && chmod -R u+rwX "$S" && rm -rf "$S"
mkdir -p "$S/.config/oku"

export HOME=$S
export XDG_CONFIG_HOME=$S/.config
export XDG_DATA_HOME=$S/.local/share
export XDG_CACHE_HOME=$S/.cache
export PATH=$root/bin:/usr/bin:/bin:/usr/sbin:/sbin
export BAT_STYLE=plain BAT_PAGING=never BAT_THEME="Catppuccin Mocha"
unset NO_COLOR
export SHELL=/bin/zsh

# .zshrc also sets the prompt, so the shell that `oku shell` starts gets it.
cat > "$HOME/.zshrc" <<EOF
eval "\$($root/bin/oku hook zsh)"
PROMPT='%F{blue}%~%f %F{magenta}❯%f '
EOF
source "$HOME/.zshrc"
cd "$S"

fixture=$demos/fixtures/$scene.sh
[ -f "$fixture" ] && source "$fixture"
printf '\033[2J\033[3J\033[H'
