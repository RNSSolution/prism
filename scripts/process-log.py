#!/usr/bin/python3

import os
import re
import datetime
import statistics
import argparse

parser = argparse.ArgumentParser(description='Extract duration of block consensus from tendermint log files.')

parser.add_argument('files', metavar='FILE', type=str, nargs='+',
                    help='Log files')

args = parser.parse_args()
print(args.files)

record=re.compile("(.)\\[([0-9-]+\\|[0-9:.]+)\\] (.*[^ ])  +(.*)")
start=re.compile("Received complete proposal block")
end=re.compile("Finalizing commit of block")


file="build/node0/prism.log"

def process_log(file):
	blockTime=None
	roundTimes = []

	with open(file,'r') as log:
		for line in log:
			line = line.strip()
			m = record.match(line)
			if m:
				lvl,tstamp,msg,param = m.groups()
				if start.match(msg):
					blockTime=datetime.datetime.strptime(tstamp, "%Y-%m-%d|%H:%M:%S.%f")
					continue
				if end.match(msg):
					commitTime = datetime.datetime.strptime(tstamp, "%Y-%m-%d|%H:%M:%S.%f")
					if blockTime is not None:
						delta = commitTime - blockTime
						roundTimes.append(delta.total_seconds())
		return roundTimes

roundTimes = []

for file in args.files:
	roundTimes.extend(process_log(file))

print("Number of completed rounds: ", len(roundTimes))
if len(roundTimes) > 0:
	print("Average round time: ", statistics.mean(roundTimes))
	print("Median round time: ", statistics.median(roundTimes))