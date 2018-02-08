#!/bin/bash
mkdir -p ./lib/staticData

go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
go get -u github.com/golang/protobuf/protoc-gen-go

protoc -I../element43/services/staticData \
-I$GOPATH/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis \
--go_out=plugins=grpc:./lib/staticData \
../element43/services/staticData/staticData.proto