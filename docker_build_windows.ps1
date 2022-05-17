 docker run --rm -v ${PWD}:/usr/src/myapp -w /usr/src/myapp -e GOOS=windows -e GOARCH=386 golang:1.17 go build -v -o ./dotfile.exe ./cmd/dotfile/main.go
 
