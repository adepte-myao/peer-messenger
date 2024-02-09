package loggers

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger interface {
	Debug(args ...interface{})
	Info(args ...interface{})
	Warn(args ...interface{})
	Error(args ...interface{})
	Fatal(args ...interface{})
}

func NewZap() (Logger, error) {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	logConfig := zap.NewProductionConfig()
	logConfig.EncoderConfig = encoderConfig
	logConfig.DisableStacktrace = true

	level := zapcore.DebugLevel
	logConfig.Level.SetLevel(level)

	coreLogger, err := logConfig.Build()
	if err != nil {
		return nil, err
	}

	return coreLogger.Sugar(), nil
}
