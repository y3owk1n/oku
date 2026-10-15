mkdir -p ~/web && cd ~/web
cp $demos/../examples/runtimes/{node,python}.toml .
cat > oku.toml <<'T'
[runtimes]
node = "./node.toml"
python = "./python.toml"

[packages]
prettier = "npm:prettier"
ruff = "pypi:ruff"
T
