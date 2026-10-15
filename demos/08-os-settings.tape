Output docs/demos/08-os-settings.gif
Output demos/out/08-os-settings.mp4
Source demos/style.tape

Hide
Type "source demos/setup.sh settings"
Enter
Wait
Show
Sleep 500ms

Type "bat ~/.config/oku/oku.toml"
Enter
Wait
Sleep 2.5s
Type "oku sync"
Enter
Wait
Sleep 1s
Type "defaults read com.example.oku-demo"
Enter
Wait
Sleep 1.5s
Type "oku rollback"
Enter
Wait
Sleep 1s
Type "defaults read com.example.oku-demo"
Enter
Wait

Sleep 3s
