Output docs/demos/04-try.gif
Output demos/out/04-try.mp4
Source demos/style.tape

Hide
Type "source demos/setup.sh try"
Enter
Wait
Show
Sleep 500ms

Type "oku shell github:BurntSushi/ripgrep github:sharkdp/fd"
Enter
Wait
Sleep 1s
Type "rg --version | head -1"
Enter
Wait
Type "exit"
Enter
Wait
Sleep 500ms
Type "rg --version"
Enter
Wait
Sleep 1.5s
Type "oku run github:sharkdp/hyperfine -- --warmup 1 --runs 5 'sleep 0.2'"
Enter
Wait
Sleep 1s
Type "oku list"
Enter
Wait

Sleep 3s
