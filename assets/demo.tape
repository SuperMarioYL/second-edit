# Asset demo: init, one accepted edit, one refused edit.
# Rendered with vhs: vhs assets/demo.tape
Output assets/demo.gif
Set Shell "bash"
Set FontSize 15
Set Width 1040
Set Height 660
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 35ms
Set CursorBlink false

Sleep 400ms
Type "go build -o bin/second-edit ./cmd/second-edit"
Enter
Sleep 1500ms
Type "go run ./testdata/hero/make_fixture.go -out demo-out"
Enter
Sleep 1200ms
Type "bin/second-edit init demo-out/hero.png --scene demo-out/hero.tscn -o demo-out/hero.asset.json"
Enter
Sleep 900ms
Type "bin/second-edit apply --asset demo-out/hero.asset.json --region helmet --patch demo-out/helmet.png --out demo-out/hero.v2.png"
Enter
Sleep 1600ms
Type "bin/second-edit apply --asset demo-out/hero.v2.asset.json --region helmet --patch demo-out/sloppy.png --out demo-out/hero.v3.png"
Enter
Sleep 1600ms
Type "bin/second-edit check --asset demo-out/hero.v2.asset.json"
Enter
Sleep 1500ms
