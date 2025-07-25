package cache

import (
	"github.com/sirupsen/logrus"
)

// Logger 定义日志接口
type Logger interface {
	Debug(args ...interface{})
	Info(args ...interface{})
	Warn(args ...interface{})
	Error(args ...interface{})
	Fatal(args ...interface{})

	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})

	WithFields(fields map[string]interface{}) Logger
}

// 默认全局日志实例
var defaultLogger Logger

// 初始化默认日志实例
func init() {
	defaultLogger = NewLogrusLogger()
}

// GetDefaultLogger 获取默认日志实例
func GetDefaultLogger() Logger {
	return defaultLogger
}

// SetDefaultLogger 设置默认日志实例
func SetDefaultLogger(logger Logger) {
	if logger != nil {
		defaultLogger = logger
	}
}

// LogrusLogger logrus适配器实现
type LogrusLogger struct {
	logger *logrus.Logger
	entry  *logrus.Entry
}

// NewLogrusLogger 创建一个新的logrus日志适配器
func NewLogrusLogger() *LogrusLogger {
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	return &LogrusLogger{
		logger: logger,
		entry:  logrus.NewEntry(logger),
	}
}

// WithFields 实现带字段的日志
func (l *LogrusLogger) WithFields(fields map[string]interface{}) Logger {
	return &LogrusLogger{
		logger: l.logger,
		entry:  l.entry.WithFields(logrus.Fields(fields)),
	}
}

// Debug 实现Debug级别日志
func (l *LogrusLogger) Debug(args ...interface{}) {
	l.entry.Debug(args...)
}

// Info 实现Info级别日志
func (l *LogrusLogger) Info(args ...interface{}) {
	l.entry.Info(args...)
}

// Warn 实现Warn级别日志
func (l *LogrusLogger) Warn(args ...interface{}) {
	l.entry.Warn(args...)
}

// Error 实现Error级别日志
func (l *LogrusLogger) Error(args ...interface{}) {
	l.entry.Error(args...)
}

// Fatal 实现Fatal级别日志
func (l *LogrusLogger) Fatal(args ...interface{}) {
	l.entry.Fatal(args...)
}

// Debugf 实现Debug级别格式化日志
func (l *LogrusLogger) Debugf(format string, args ...interface{}) {
	l.entry.Debugf(format, args...)
}

// Infof 实现Info级别格式化日志
func (l *LogrusLogger) Infof(format string, args ...interface{}) {
	l.entry.Infof(format, args...)
}

// Warnf 实现Warn级别格式化日志
func (l *LogrusLogger) Warnf(format string, args ...interface{}) {
	l.entry.Warnf(format, args...)
}

// Errorf 实现Error级别格式化日志
func (l *LogrusLogger) Errorf(format string, args ...interface{}) {
	l.entry.Errorf(format, args...)
}

// Fatalf 实现Fatal级别格式化日志
func (l *LogrusLogger) Fatalf(format string, args ...interface{}) {
	l.entry.Fatalf(format, args...)
}
