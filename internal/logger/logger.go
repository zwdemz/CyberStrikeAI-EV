package logger

import (
	"errors"
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger struct {
	*zap.Logger
	outputFile *os.File
	closeOnce  sync.Once
	closeErr   error
}

func New(level, output string, diagnostics ...DiagnosticOptions) *Logger {
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapLevel)
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var writeSyncer zapcore.WriteSyncer
	var outputFile *os.File
	if output == "stdout" {
		writeSyncer = zapcore.AddSync(os.Stdout)
	} else if output == "stderr" {
		writeSyncer = zapcore.AddSync(os.Stderr)
	} else {
		file, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			writeSyncer = zapcore.AddSync(os.Stdout)
		} else {
			outputFile = file
			writeSyncer = zapcore.AddSync(file)
		}
	}

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(config.EncoderConfig),
		writeSyncer,
		zapLevel,
	)

	options := DiagnosticOptions{}
	if len(diagnostics) > 0 {
		options = diagnostics[0]
	}
	if !options.Disabled {
		// The diagnostic threshold is independent of the primary output level.
		core = zapcore.NewTee(core, zapcore.NewCore(
			zapcore.NewJSONEncoder(config.EncoderConfig),
			newDailyWriter(options), zapcore.WarnLevel,
		))
	}

	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return &Logger{Logger: logger, outputFile: outputFile}
}

// Close flushes and releases the primary file opened by New after logging stops.
// Repeated calls return the first flush/close error; stdout and stderr remain open.
func (l *Logger) Close() error {
	if l == nil || l.outputFile == nil {
		return nil
	}
	l.closeOnce.Do(func() {
		l.closeErr = errors.Join(l.Logger.Sync(), l.outputFile.Close())
	})
	return l.closeErr
}

func (l *Logger) Fatal(msg string, fields ...interface{}) {
	zapFields := make([]zap.Field, 0, len(fields))
	for _, f := range fields {
		switch v := f.(type) {
		case error:
			zapFields = append(zapFields, zap.Error(v))
		default:
			zapFields = append(zapFields, zap.Any("field", v))
		}
	}
	l.Logger.Fatal(msg, zapFields...)
}
