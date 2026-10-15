Output docs/demos/06-dotfiles.gif
Output demos/out/06-dotfiles.mp4
Source demos/style.tape

Hide
Type "source demos/setup.sh dotfiles"
Enter
Wait
Show
Sleep 500ms

Type "bat oku.toml files/ghostty.tmpl"
Enter
Wait
Sleep 2s
Type "oku sync"
Enter
Wait
Sleep 1s
Type "readlink ~/.vimrc && cat ~/.config/ghostty/config"
Enter
Wait
Sleep 1.5s
Type "sed -i '' 's/catppuccin-mocha/tokyonight/' oku.toml && oku sync"
Enter
Wait
Sleep 1s
Type "cat ~/.config/ghostty/config"
Enter
Wait

Sleep 3s
