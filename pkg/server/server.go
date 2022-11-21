package server

import (
	"embed"
	"io"
	"net/http"
	"time"

	apiv1 "github.com/deejross/openshift-cli-manager/pkg/server/v1"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	//go:embed resources/*
	resources embed.FS
)

type HTTPHandler struct {
	log logr.Logger
	mux *http.ServeMux
	v1  *apiv1.V1
}

func NewHTTPHandler(cli client.Client, logger logr.Logger) http.Handler {
	h := &HTTPHandler{
		log: logger.WithName("API"),
		mux: http.NewServeMux(),
	}

	h.v1 = apiv1.NewV1(cli, h.log)
	h.v1.RegisterRoutes(h.mux)

	h.mux.Handle("/resources/", http.FileServer(http.FS(resources)))
	h.mux.HandleFunc("/", h.handleHome)

	return h
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	wrapped := &responseWriter{ResponseWriter: w}
	h.mux.ServeHTTP(wrapped, r)

	h.log.Info(
		"request",
		"status", wrapped.StatusCode(),
		"method", r.Method,
		"path", r.URL.Path,
		"size", wrapped.size,
		"duration", time.Since(start),
		"client", getRemoteAddr(r),
	)
}

func (h *HTTPHandler) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		f, err := resources.Open("resources/index.html")
		if err == nil {
			defer f.Close()
			if _, err := io.Copy(w, f); err != nil {
				h.log.Error(err, "reading index.html")
			}
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
}

func getRemoteAddr(r *http.Request) string {
	addr := r.Header.Get("X-Real-Ip")
	if len(addr) > 0 {
		return addr
	}

	addr = r.Header.Get("X-Forwarded-For")
	if len(addr) > 0 {
		return addr
	}

	return r.RemoteAddr
}

type responseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (r *responseWriter) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseWriter) Write(b []byte) (int, error) {
	r.size += len(b)
	return r.ResponseWriter.Write(b)
}

func (r *responseWriter) StatusCode() int {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.status
}
