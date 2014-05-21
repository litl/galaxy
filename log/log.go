package log

import (
	"io"
	golog "log"
	"os"

	"github.com/daviddengcn/go-colortext"
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
	if l.Level < DEBUG {
		return
	}
	l.Print(string(p))
	return len(p), nil
}

func Debug(v ...interface{})                 { DefaultLogger.Debug(v...) }
func Debugf(format string, v ...interface{}) { DefaultLogger.Debugf(format, v...) }
func Fatal(v ...interface{}) {
	ct.ChangeColor(ct.Red, true, ct.None, false)
	DefaultLogger.Fatal(v...)
	ct.ResetColor()
}
func Fatalf(format string, v ...interface{}) {
	ct.ChangeColor(ct.Red, true, ct.None, false)
	DefaultLogger.Fatalf(format, v...)
	ct.ResetColor()
}
func Fatalln(v ...interface{}) {
	ct.ChangeColor(ct.Red, true, ct.None, false)
	DefaultLogger.Fatalln(v...)
	ct.ResetColor()
}
func Panic(v ...interface{}) {
	ct.ChangeColor(ct.Red, true, ct.None, false)
	DefaultLogger.Panic(v...)
	ct.ResetColor()
}
func Panicf(format string, v ...interface{}) {
	ct.ChangeColor(ct.Red, true, ct.None, false)
	DefaultLogger.Panicf(format, v...)
	ct.ResetColor()
}
func Panicln(v ...interface{}) {
	ct.ChangeColor(ct.Red, true, ct.None, false)
	DefaultLogger.Panicln(v...)
	ct.ResetColor()
}

func Error(v ...interface{}) {
	ct.ChangeColor(ct.Red, true, ct.None, false)
	DefaultLogger.Print(v...)
	ct.ResetColor()
}
func Errorf(format string, v ...interface{}) {
	ct.ChangeColor(ct.Red, true, ct.None, false)
	DefaultLogger.Printf(format, v...)
	ct.ResetColor()
}
func Errorln(v ...interface{}) {
	ct.ChangeColor(ct.Red, true, ct.None, false)
	DefaultLogger.Println(v...)
	ct.ResetColor()
}

func Warn(v ...interface{}) {
	ct.ChangeColor(ct.Yellow, true, ct.None, false)
	DefaultLogger.Print(v...)
	ct.ResetColor()
}
func Warnf(format string, v ...interface{}) {
	ct.ChangeColor(ct.Yellow, true, ct.None, false)
	DefaultLogger.Printf(format, v...)
	ct.ResetColor()
}
func Warnln(v ...interface{}) {
	ct.ChangeColor(ct.Yellow, true, ct.None, false)
	DefaultLogger.Println(v...)
	ct.ResetColor()
}

func Print(v ...interface{})                 { DefaultLogger.Print(v...) }
func Printf(format string, v ...interface{}) { DefaultLogger.Printf(format, v...) }
func Println(v ...interface{})               { DefaultLogger.Println(v...) }
