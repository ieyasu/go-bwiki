#!/bin/bash
SRC="$(pwd)/$(echo $BASH_SOURCE | sed 's/^\.\///')"
WIKI_ROOT=`dirname $SRC`
cd $WIKI_ROOT
echo "in $(pwd), starting wiki..."
exec ./go-bwiki &>> log
