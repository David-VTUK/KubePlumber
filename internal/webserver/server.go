package webserver

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/David-VTUK/KubePlumber/common"
	log "github.com/sirupsen/logrus"
)

//go:embed index.html
var indexHTML []byte

// Server wraps the HTTP server and holds a reference to the live results store.
type Server struct {
	store *common.ResultsStore
	port  int
	srv   *http.Server
}

// New creates a Server that will expose results on the given port.
func New(store *common.ResultsStore, port int) *Server {
	return &Server{store: store, port: port}
}

// Start launches the HTTP server in a background goroutine.
func (s *Server) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/results", s.handleResults)

	s.srv = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	go func() {
		log.Infof("Web UI available at http://localhost:%d", s.port)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("Web server error: %v", err)
		}
	}()
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML) //nolint:errcheck
}

func (s *Server) handleResults(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	data, err := json.Marshal(s.store)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data) //nolint:errcheck
}
