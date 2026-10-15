# A machines repo with a list and its lock. The lock comes from a sync in a
# scratch home, so the recorded sync still downloads every package.
R=~/machines
mkdir -p $R/files
printf 'set number\n' > $R/files/vimrc
cat > $R/oku.toml <<'T'
[packages]
ripgrep = "github:BurntSushi/ripgrep"
fd = "github:sharkdp/fd"
jq = "aqua:jqlang/jq"
stats = { ref = "cask:stats", when = { os = "darwin" } }

[files]
"{{home}}/.vimrc" = { link = "./files/vimrc" }
T
G=$root/.lockgen
( export HOME=$G XDG_CONFIG_HOME=$G/config XDG_DATA_HOME=$G/data XDG_CACHE_HOME=$G/cache
  mkdir -p $XDG_CONFIG_HOME && cp -R $R $XDG_CONFIG_HOME/oku
  oku sync >/dev/null 2>&1 && cp $XDG_CONFIG_HOME/oku/oku.lock $R/ )
chmod -R u+rwX $G && rm -rf $G
(cd $R && git init -q && git add . && git -c user.name=demo -c user.email=demo@example.com commit -qm "my machines")
rm -rf ~/.config/oku
