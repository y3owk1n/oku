Output docs/demos/12-publish.gif
Output demos/out/12-publish.mp4
Source demos/style.tape

Hide
Type "source demos/setup.sh publish"
Enter
Wait
Show
Sleep 500ms

Type "oku manifest init --from sharkdp/hyperfine"
Enter
Wait
Sleep 1s
Type "bat -r 1:16 oku.pkg.toml"
Enter
Wait
Sleep 2s
Type "oku manifest lint && oku manifest test"
Enter
Wait

Sleep 3s
