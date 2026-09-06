#!/bin/sh
IMG=muaddib/creaves-console

docker buildx build --platform linux/amd64,linux/arm64 --push -t $IMG .
