package httpapi

import (
	"log"
	"net/http"
	"time"
)

// LoggingMiddleware records request metadata and duration without reading
// request bodies, keeping prompts, tool arguments, and credentials out of logs.
func LoggingMiddleware(logger *log.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		logger.Printf("http request method=%s path=%s status=%d bytes=%d duration=%s remote=%s", r.Method, r.URL.Path, recorder.status, recorder.bytes, time.Since(started), r.RemoteAddr)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}
