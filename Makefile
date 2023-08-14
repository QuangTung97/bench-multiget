.PHONY: build run generate

build:
	go build -o bin/main

run:
	GOGC=off GOMEMLIMIT=512 ./bin/main

generate:
	protoc -I. --gofast_out=paths=source_relative:"./pb" cache.proto