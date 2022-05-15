#!/bin/bash

name="log-collect"

build(){
	go build -o $name main.go 
	chmod 755 $name
	mkdir ./$name-$GOOS-$GOARCH 
	mv $name ./$name-$GOOS-$GOARCH 
	cp *.yml ./$name-$GOOS-$GOARCH 
	cp README.md ./$name-$GOOS-$GOARCH 
	tar zcf $name-$GOOS-$GOARCH.tar.gz $name-$GOOS-$GOARCH 
	rm -rf $name-$GOOS-$GOARCH 
}

export CGO_ENABLED=0
export GOOS=linux

# amd64
export GOARCH=amd64
echo $name-$GOOS-$GOARCH
build


