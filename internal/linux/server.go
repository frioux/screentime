package linux

import (
	"context"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
)

// Server provides the Roku-compatible HTTP API
type Server struct {
	detector *Detector
	config   *Config
	server   *http.Server
}

// activeAppResponse matches the Roku XML format
type activeAppResponse struct {
	XMLName xml.Name `xml:"active-app"`
	App     struct {
		ID   string `xml:"id,attr"`
		Name string `xml:",chardata"`
	} `xml:"app"`
}

// NewServer creates a new HTTP server
func NewServer(cfg *Config, detector *Detector) *Server {
	return &Server{
		detector: detector,
		config:   cfg,
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/query/active-app", s.handleActiveApp)
	mux.HandleFunc("/health", s.handleHealth)

	s.server = &http.Server{
		Addr:    s.config.Listen,
		Handler: mux,
	}

	log.Printf("Starting Linux agent on %s", s.config.Listen)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *Server) handleActiveApp(w http.ResponseWriter, r *http.Request) {
	activity := s.detector.Detect()

	resp := activeAppResponse{}
	resp.App.ID = activity.ID
	resp.App.Name = activity.Name

	w.Header().Set("Content-Type", "application/xml")

	// Write XML declaration
	fmt.Fprint(w, xml.Header)

	enc := xml.NewEncoder(w)
	if err := enc.Encode(resp); err != nil {
		log.Printf("error encoding response: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintln(w, "ok")
}


