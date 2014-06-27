package log

import (
	"io"
	golog "log"
	"os"

	"github.com/fatih/color"
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

var (
	red      = color.New(color.FgRed).SprintFunc()
	redln    = color.New(color.FgRed).SprintlnFunc()
	redf     = color.New(color.FgRed).SprintfFunc()
	yellow   = color.New(color.FgYellow).SprintFunc()
	yellowln = color.New(color.FgYellow).SprintlnFunc()
	yellowf  = color.New(color.FgYellow).SprintfFunc()
)

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
	if l.Level < DEBUG {
		return
	}
	l.Print(string(p))
	return len(p), nil
}

func Debug(v ...interface{})                 { DefaultLogger.Debug(v...) }
func Debugf(format string, v ...interface{}) { DefaultLogger.Debugf(format, v...) }
func Fatal(v ...interface{}) {
	DefaultLogger.Fatal(red(v...))
}
func Fatalf(format string, v ...interface{}) {
	DefaultLogger.Fatal(redf(format, v...))
}
func Fatalln(v ...interface{}) {
	DefaultLogger.Fatal(redln(v...))
}
func Panic(v ...interface{}) {
	DefaultLogger.Panic(red(v...))
}
func Panicf(format string, v ...interface{}) {
	DefaultLogger.Panic(redf(format, v...))
}
func Panicln(v ...interface{}) {
	DefaultLogger.Panic(redln(v...))
}

func Error(v ...interface{}) {
	DefaultLogger.Print(red(v...))
}
func Errorf(format string, v ...interface{}) {
	DefaultLogger.Print(redf(format, v...))
}
func Errorln(v ...interface{}) {
	DefaultLogger.Print(redln(v...))
}

func Warn(v ...interface{}) {
	DefaultLogger.Print(yellow(v...))
}
func Warnf(format string, v ...interface{}) {
	DefaultLogger.Print(yellowf(format, v...))
}
func Warnln(v ...interface{}) {
	DefaultLogger.Print(yellowln(v...))
}

func Print(v ...interface{})                 { DefaultLogger.Print(v...) }
func Printf(format string, v ...interface{}) { DefaultLogger.Printf(format, v...) }
func Println(v ...interface{})               { DefaultLogger.Println(v...) }
