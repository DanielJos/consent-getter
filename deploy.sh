#!/bin/bash

declare -a list=("consent-deploy" "consent-deploy-us-east-1")
declare -a regions=("eu-west-2" "us-east-1")
declare -a lambdaname=("getter2" "getter-us-east-1")


for i in "${!list[@]}"; do
    echo "Bucket $i: ${list[i]}"

    aws s3 --region "${regions[i]}" cp ./builds/getter.zip "s3://${list[i]}/getter.zip"

    aws lambda --region "${regions[i]}" update-function-code \
        --function-name "${lambdaname[i]}" \
        --s3-bucket "${list[i]}" \
        --s3-key getter.zip \
        --output text

done

echo "Done."