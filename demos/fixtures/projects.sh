mkdir -p ~/api
cat > ~/api/oku.toml <<'T'
[packages]
just = "github:casey/just"

[env]
DATABASE_URL = "postgres://localhost/api_dev"
T
