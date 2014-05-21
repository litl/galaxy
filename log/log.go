package log

import (
	"io"
	golog "log"
	"os"
)

const (
	ERROR = iota
	INFO
	WARN
	DEBUG
)

type Logger struct {
	golog.Logger
	Level  int
	Prefix string
}

func New(out io.Writer, prefix string, level int) *Logger {
	l := &Logger{
		Level:  level,
		Prefix: prefix,
	}
	l.Logger = *(golog.New(out, prefix, golog.LstdFlags))
	return l
}

var DefaultLogger = New(os.Stderr, "", INFO)

func (l *Logger) Debug(v ...interface{}) {
	if l.Level < DEBUG {
		return
	}
	l.Println(v...)
}

func (l *Logger) Debugf(fmt string, v ...interface{}) {
	if l.Level < DEBUG {
		return
	}
	l.Printf(fmt, v...)
}

func (l *Logger) Write(p []byte) (n int, err error) {
	l.Print(string(p))
	return len(p), nil
}

func Debug(v ...interface{})                 { DefaultLogger.Debug(v...) }
func Debugf(format string, v ...interface{}) { DefaultLogger.Debugf(format, v...) }
func Fatal(v ...interface{})                 { DefaultLogger.Fatal(v...) }
func Fatalf(format string, v ...interface{}) { DefaultLogger.Fatalf(format, v...) }
func Fatalln(v ...interface{})               { DefaultLogger.Fatalln(v...) }
func Panic(v ...interface{})                 { DefaultLogger.Panic(v...) }
func Panicf(format string, v ...interface{}) { DefaultLogger.Panicf(format, v...) }
func Panicln(v ...interface{})               { DefaultLogger.Panicln(v...) }
func Print(v ...interface{})                 { DefaultLogger.Print(v...) }
func Printf(format string, v ...interface{}) { DefaultLogger.Printf(format, v...) }
func Println(v ...interface{})               { DefaultLogger.Println(v...) }
