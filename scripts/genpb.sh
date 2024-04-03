#!/bin/bash

git clone git@github.com:istio/api.git
git clone git@github.com:googleapis/googleapis.git

cd ../

protoc \
--proto_path=scripts/api \
--proto_path=scripts/googleapis \
--proto_path=api/efwrapper/v1/ \
--go_out=. \
--go-grpc_out=. \
api/efwrapper/v1/*.proto
