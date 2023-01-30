#!/bin/bash

aws lambda update-function-code \
    --function-name getter2 \
    --zip-file fileb://builds/getter.zip