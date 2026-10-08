package middleware

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"pulsar/internal/logger"
	"strings"
	"time"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
	}
	return hijacker.Hijack()
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		lrw := &loggingResponseWriter{
			ResponseWriter: w,
		}

		next.ServeHTTP(lrw, r)

		duration := time.Since(start)

		isWS := strings.EqualFold(r.Header.Get("Upgrade"), "websocket")

		logger.Logf(
			r.Method,
			"%s %s status(%d) %v websocket=%v",
			r.URL.Path,
			r.Proto,
			lrw.status,
			duration,
			isWS,
		)
	})
}
