package logger

import (
	"fmt"
	"os"
	"time"
)

var (
	colorReset = "\033[0m"
	colorLog   = "\033[37m"    // White
	colorDebug = "\033[35m"    // Magenta
	colorInfo  = "\033[34m"    // Blue
	colorWarn  = "\033[33m"    // Yellow
	colorError = "\033[31m"    // Red
	colorFatal = "\033[31;40m" // Red foreground with Black background
)

func writeLog(color, level, message string) {
	timestamp := time.Now().Format("2006/01/02 15:04:05")
	if len(message) > 0 && message[len(message)-1] == '\n' {
		message = message[:len(message)-1]
	}
	fmt.Fprintf(os.Stdout, "%s%s [%s] %s%s\n", color, timestamp, level, message, colorReset)
}

func Log(name string, args ...any) {
	writeLog(colorLog, name, fmt.Sprint(args...))
}

func Logf(name string, format string, args ...any) {
	writeLog(colorLog, name, fmt.Sprintf(format, args...))
}

func Logln(name string, args ...any) {
	writeLog(colorLog, name, fmt.Sprintln(args...))
}

func Debug(args ...any) {
	writeLog(colorDebug, "DEBUG", fmt.Sprint(args...))
}

func Debugf(format string, args ...any) {
	writeLog(colorDebug, "DEBUG", fmt.Sprintf(format, args...))
}

func Debugln(args ...any) {
	writeLog(colorDebug, "DEBUG", fmt.Sprintln(args...))
}

func Info(args ...any) {
	writeLog(colorInfo, "INFO", fmt.Sprint(args...))
}

func Infof(format string, args ...any) {
	writeLog(colorInfo, "INFO", fmt.Sprintf(format, args...))
}

func Infoln(args ...any) {
	writeLog(colorInfo, "INFO", fmt.Sprintln(args...))
}

func Warn(args ...any) {
	writeLog(colorWarn, "WARN", fmt.Sprint(args...))
}

func Warnf(format string, args ...any) {
	writeLog(colorWarn, "WARN", fmt.Sprintf(format, args...))
}

func Warnln(args ...any) {
	writeLog(colorWarn, "WARN", fmt.Sprintln(args...))
}

func Error(args ...any) {
	writeLog(colorError, "ERROR", fmt.Sprint(args...))
}

func Errorf(format string, args ...any) {
	writeLog(colorError, "ERROR", fmt.Sprintf(format, args...))
}

func Errorln(args ...any) {
	writeLog(colorError, "ERROR", fmt.Sprintln(args...))
}

func Fatal(args ...any) {
	writeLog(colorFatal, "FATAL", fmt.Sprint(args...))
	os.Exit(1)
}

func Fatalf(format string, args ...any) {
	writeLog(colorFatal, "FATAL", fmt.Sprintf(format, args...))
	os.Exit(1)
}

func Fatalln(args ...any) {
	writeLog(colorFatal, "FATAL", fmt.Sprintln(args...))
	os.Exit(1)
}

func init() {
	if os.Getenv("ENV") != "development" {
		colorReset = ""
		colorDebug = ""
		colorInfo = ""
		colorWarn = ""
		colorError = ""
		colorFatal = ""
	}
}
