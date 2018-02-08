#!/bin/bash
mkdir -p ./lib/staticData

protoc -I/usr/local/include -I. \
-I../element43/services/staticData \
-I$GOPATH/src \
-I$GOPATH/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis \
--go_out=plugins=grpc:./lib/staticData \
../element43/services/staticData/staticData.proto