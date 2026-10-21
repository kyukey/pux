Output assets/demo.gif

Require pux

Set Shell "bash"
Set FontSize 20
Set Width 1000
Set Height 560
Set Padding 24
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful
Set BorderRadius 10
Set TypingSpeed 60ms

Env PUX_DIR "/tmp/pux-demo"

Hide
Type `export PS1="$ " && rm -rf /tmp/pux-demo && clear`
Enter
Sleep 500ms
Show

Type `pux new hi 'echo hi $1'`
Sleep 400ms
Enter
Sleep 1s

Type "pux hi there"
Sleep 400ms
Enter
Sleep 1.5s

Type "pux new deploy"
Sleep 400ms
Enter
Sleep 1s
Type `echo "building..."`
Enter
Type "sleep 1"
Enter
Type `echo "deployed to $1"`
Sleep 800ms
Ctrl+D
Sleep 1s

Type "pux deploy prod"
Sleep 400ms
Enter
Sleep 2.5s

Type "pux ls"
Sleep 400ms
Enter
Sleep 1.5s

Type "pux find"
Sleep 400ms
Enter
Sleep 800ms
Type@200ms "dep"
Sleep 1s
Enter
Sleep 2.5s

Hide
Type "rm -rf /tmp/pux-demo"
Enter
