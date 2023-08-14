.PHONY: all generate

all:
	go run main.go

generate:
	protoc -I. --go_out=paths=source_relative:"./pb" cache.proto