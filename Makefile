.PHONY: compile
PROJECTNAME := $(shell basename "$(PWD)")

GOBASE := $(shell pwd)
GOPATH := $(GOBASE)/vendor:$(GOBASE)
GOBIN := $(GOBASE)/bin
GOFILES := $(wildcard *.go)

PID := /tmp/.$(PROJECTNAME).pid

#
#PROTOC_GEN_GO := $(GOPATH)/bin/protoc-gen-go
#
#$(PROTOC_GEN_GO):
#	go get -u github.com/golang/protobuf/protoc-gen-go
#

#
#go-generate:
#	@echo "  >  Generating dependency files..."
#	@GOPATH=$(GOPATH) GOBIN=$(GOBIN) go generate $(generate)

stop-server:
	@-touch $(PID)
	@-kill `cat $(PID)` 2> /dev/null || true
	@-rm $(PID)

protocol.pb.go:
	#protoc --go_out=plugins=grpc:api/protocol.proto
	protoc --go_out=plugins=grpc:./ --go_opt=paths=source_relative api/protocol.proto
#
## This is a "phony" target - an alias for the above command, so "make compile"
## still works.
compile: protocol.pb.go


help: Makefile
	@echo
	@echo " command run in "$(PROJECTNAME)":"

