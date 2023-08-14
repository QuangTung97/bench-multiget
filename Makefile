.PHONY: all generate

all:
	go run main.go

generate:
	protoc -I. --gofast_out=paths=source_relative:"./pb" cache.proto