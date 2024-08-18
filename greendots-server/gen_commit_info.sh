#!/bin/sh
printf "build\tvcs=git\nbuild\tvcs.revision=$(cat ../.git/$(cut -f2 -d' ' < ../.git/HEAD))\n" > commit_info.txt
