#!/bin/sh

# gawk replace EOL characters with '\n', adds filename to each line and transforms timestamps from 'D[2019-05-25|00:11:22.333] ' into 'D|2019-05-25 00:11:22.333|'
# sort sorts by the timestamp field
# The first sed transforms leading file name in each line into VM number
# The second sed replaces '\n' back into EOL

gawk '/^[DIE]\[/ {print FILENAME"|"line; line=gensub(/^([EID])\[([^|]+)\|([0-9.:]+)\] /,"\\1|\\2 \\3|",1); next} {line=line"\\n"$0;next;} END{print FILENAME"|"line}' build/node*/prism-*.log  |\
 sort -t '|' -k 3 |\
 sed -e 's/^build.node[0-9]\+.prism-\([0-9]\+\).log/\1/;' |\
 sed -e 's/\\n/\n/g'
