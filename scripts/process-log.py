#!/usr/bin/python3

import os
import re
import datetime
import statistics
import argparse

parser = argparse.ArgumentParser(description='Extract duration of block consensus from tendermint log files.')

parser.add_argument('files', metavar='FILE', type=str, nargs='+',
                    help='Log files')
parser.add_argument('-c','--commits', action='store_true', help="Count timestamps between state commits")

args = parser.parse_args()
print(args.files)

record = re.compile("(.)\\[([0-9-]+\\|[0-9:.]+)\\] (.*[^ ])  +(.*)")
start = re.compile("Received complete proposal block")
if args.commits:
	print("Use committed state messages")
	end = re.compile("Committed state")
else:
	end = re.compile("Finalizing commit of block")


def process_log(file):
	blockTime=None
	roundTimes = []

	with open(file,'r') as log:
		for line in log:
			line = line.strip()
			m = record.match(line)
			if m:
				lvl,tstamp,msg,param = m.groups()
				if end.match(msg):
					commitTime = datetime.datetime.strptime(tstamp, "%Y-%m-%d|%H:%M:%S.%f")
					if blockTime is not None:
						delta = commitTime - blockTime
						roundTimes.append(delta.total_seconds())
					if args.commits:
						blockTime = commitTime
						continue
				if start.match(msg):
					blockTime=datetime.datetime.strptime(tstamp, "%Y-%m-%d|%H:%M:%S.%f")
					continue
		return roundTimes

roundTimes = []

for file in args.files:
	roundTimes.extend(process_log(file))

print("Number of completed rounds: ", len(roundTimes))
if len(roundTimes) > 0:
	print("Average round time: ", statistics.mean(roundTimes))
	print("Median round time: ", statistics.median(roundTimes))