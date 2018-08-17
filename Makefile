SHELL := /bin/bash

all: image

dev:
	go install -v cmd/...

image:
	GOOS=linux GOARCH=amd64 go build -o ./docker/agent ./cmd/agent
	cd docker && docker build -t cert-sync:latest . && cd -
	rm ./docker/agent

test:
	go test -v $(shell go list ./... | grep -v /vendor/)
