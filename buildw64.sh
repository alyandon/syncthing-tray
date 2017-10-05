#!/bin/sh

CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 CGO_ENABLED=1 go build -i -v -ldflags "-H=windowsgui -X main.VersionStr=$versionStr -X main.BuildUnixTime=$versionDate" -o ./windows32/syncthing-tray.exe
