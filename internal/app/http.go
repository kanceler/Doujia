package app

import (
	"net/http"

	"devflow/internal/doujiagit"
)

func (b *Bootstrap) NewHTTPHandler() http.Handler {
	mux := http.NewServeMux()
	b.RegisterHTTPRoutes(mux)
	return mux
}

func (b *Bootstrap) RegisterHTTPRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	b.RegisterDebugRoutes(mux)
}

func (b *Bootstrap) RegisterDebugRoutes(mux *http.ServeMux) {
	mux.Handle("/debug/doujiagit/", doujiagit.NewDebugHandler(b.Internals.DoujiaGitRepository))
}
