Output docs/demos/07-secrets.gif
Output demos/out/07-secrets.mp4
Source demos/style.tape

Hide
Type "source demos/setup.sh secrets"
Enter
Wait
Show
Sleep 500ms

Type "head -3 secrets/secrets.yaml"
Enter
Wait
Sleep 1.5s
Type "bat oku.toml files/gh-hosts.tmpl"
Enter
Wait
Sleep 2s
Type "oku sync"
Enter
Wait
Sleep 1s
Type "cat ~/.config/gh/hosts.yml"
Enter
Wait

Sleep 3s
