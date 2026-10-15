Output docs/demos/10-projects.gif
Output demos/out/10-projects.mp4
Source demos/style.tape

Hide
Type "source demos/setup.sh projects"
Enter
Wait
Show
Sleep 500ms

Type "cd ~/api"
Enter
Wait
Sleep 1s
Type "bat oku.toml"
Enter
Wait
Sleep 1.5s
Type "oku allow && oku sync"
Enter
Wait
Sleep 1s
Type "just --version && echo $DATABASE_URL"
Enter
Wait
Sleep 1.5s
Type "cd ~"
Enter
Wait
Type "just --version; echo DATABASE_URL=$DATABASE_URL"
Enter
Wait

Sleep 3s
