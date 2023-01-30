#!/bin/bash

# go get github.com/aws/aws-lambda-go/lambda

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ./builds/getter getter.go
zip ./builds/getter.zip getter


# cd ../handler && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o getter getter.go