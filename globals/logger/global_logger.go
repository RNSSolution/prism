package logger

import (
	"github.com/ColorPlatform/prism/libs/log"
)

var (
	logger log.Logger
)

func SetLogger(log log.Logger) {
	logger = log
}

func Logger() log.Logger {
	return logger
}
