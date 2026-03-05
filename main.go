package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/joho/godotenv"
)

//go:embed dashboard.html
var dashboardHTML []byte

type Server struct {
	config    *Config
	scheduler *Scheduler
	server    *http.Server
}

func NewServer(cfg *Config) (*Server, error) {
	client, err := createHTTPClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	scheduler := NewScheduler(cfg, client)

	return &Server{
		config:    cfg,
		scheduler: scheduler,
	}, nil
}

func createHTTPClient() (tls_client.HttpClient, error) {
	options := []tls_client.HttpClientOption{
		tls_client.WithClientProfile(profiles.Firefox_120),
		tls_client.WithTimeoutSeconds(20),
	}
	return tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
}

func (s *Server) registerRoutes() {
	http.HandleFunc("/", s.handleDashboard)
	http.HandleFunc("/api/data", s.handleData)
	http.HandleFunc("/api/refresh", s.handleRefresh)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(dashboardHTML)
}

func (s *Server) handleData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.scheduler.GetCache())
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	s.scheduler.fetchAll()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.scheduler.GetCache())
}

func (s *Server) Start() error {
	s.registerRoutes()
	s.scheduler.Start()

	addr := fmt.Sprintf(":%d", s.config.ServerPort)
	s.server = &http.Server{
		Addr:    addr,
		Handler: http.DefaultServeMux,
	}

	log.Printf("Server starting on http://localhost%s", addr)
	return s.server.ListenAndServe()
}

func (s *Server) Stop() {
	log.Println("Shutting down scheduler...")
	s.scheduler.Stop()

	if s.server != nil {
		log.Println("Shutting down HTTP server...")
		if err := s.server.Close(); err != nil {
			log.Printf("Error closing server: %v", err)
		}
	}
}

func main() {
	godotenv.Load()

	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	log.Printf("Loading configuration from %s", *configPath)
	config, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	server, err := NewServer(config)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil {
			serverErr <- err
		}
	}()

	select {
	case <-stop:
		log.Println("Received shutdown signal, stopping server...")
		server.Stop()
		log.Println("Server stopped")
	case err := <-serverErr:
		if err == http.ErrServerClosed {
			log.Println("Server closed gracefully")
		} else {
			log.Fatalf("Server error: %v", err)
		}
	}
}
