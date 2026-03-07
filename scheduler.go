package main

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tls_client "github.com/bogdanfinn/tls-client"
)

type Scheduler struct {
	config      *Config
	client      tls_client.HttpClient
	cache       *CacheData
	cacheMu     sync.RWMutex
	fetchLock   atomic.Bool
	ticker      *time.Ticker
	stopChan    chan struct{}
	stopOnce    sync.Once
	claudeOrg   *string
	claudeOrgID *string
}

func NewScheduler(cfg *Config, client tls_client.HttpClient) *Scheduler {
	return &Scheduler{
		config: cfg,
		client: client,
		cache: &CacheData{
			Codex:  &ProviderData{Status: "pending"},
			Kimi:   &ProviderData{Status: "pending"},
			Claude: &ProviderData{Status: "pending"},
			Zai:    &ProviderData{Status: "pending"},
		},
		stopChan: make(chan struct{}),
	}
}

func (s *Scheduler) fetchAll() {
	if !s.fetchLock.CompareAndSwap(false, true) {
		fmt.Println("Fetch already in progress")
		return
	}
	defer s.fetchLock.Store(false)

	now := time.Now().UTC()

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	// Fetch Kimi (if enabled)
	if s.config.Providers.Kimi.Enabled {
		kimiData, kimiErr := fetchKimi(s.client, s.config.Providers.Kimi)
		s.cache.Kimi = s.resolveProviderResult(kimiData, kimiErr, s.cache.Kimi)
	}

	// Fetch ZAI (if enabled)
	if s.config.Providers.Zai.Enabled {
		zaiData, zaiErr := fetchZAI(s.client, s.config.Providers.Zai.APIKey)
		s.cache.Zai = s.resolveProviderResult(zaiData, zaiErr, s.cache.Zai)
	}

	// Fetch Codex (if enabled)
	if s.config.Providers.Codex.Enabled {
		codexData, codexErr := s.fetchCodex()
		s.cache.Codex = s.resolveProviderResult(codexData, codexErr, s.cache.Codex)
	}

	// Fetch Claude (if enabled)
	if s.config.Providers.Claude.Enabled {
		claudeData, claudeOrg, claudeErr := fetchClaude(s.client, flattenCookies(s.config.Providers.Claude.Cookies), s.claudeOrgID)
		s.cache.Claude = s.resolveProviderResult(claudeData, claudeErr, s.cache.Claude)
		if claudeErr == nil && claudeOrg != nil && strings.TrimSpace(*claudeOrg) != "" {
			s.claudeOrgID = claudeOrg
		}
	}

	s.cache.LastFetch = now.Format(time.RFC3339)
	s.cache.NextRefreshAt = now.Add(s.config.RefreshInterval).Format(time.RFC3339)
}

func (s *Scheduler) fetchCodex() (*ProviderData, error) {
	if s.config.Providers.Codex.OAuth == nil || s.config.Providers.Codex.OAuth.TokenFile == "" {
		return nil, fmt.Errorf("codex oauth not configured (set codex.oauth.token_file in config.yaml)")
	}

	return fetchCodexViaOAuth(s.client, s.config.Providers.Codex.OAuth.TokenFile)
}

func (s *Scheduler) resolveProviderResult(data *ProviderData, err error, prev *ProviderData) *ProviderData {
	if err == nil {
		if data == nil {
			return &ProviderData{Status: "error", Error: "empty provider response"}
		}
		if data.Status == "ok" {
			data.LastSuccess = time.Now().UTC().Format(time.RFC3339)
		}
		return data
	}

	if prev != nil && (prev.Status == "ok" || prev.LastSuccess != "") {
		stale := *prev
		stale.Status = "stale"
		stale.Error = err.Error()
		return &stale
	}

	return &ProviderData{Status: "error", Error: err.Error()}
}

func (s *Scheduler) Start() {
	s.fetchAll()

	s.ticker = time.NewTicker(s.config.RefreshInterval)
	go func() {
		for {
			select {
			case <-s.ticker.C:
				s.fetchAll()
			case <-s.stopChan:
				return
			}
		}
	}()
}

func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		if s.ticker != nil {
			s.ticker.Stop()
		}
		close(s.stopChan)
	})
}

func (s *Scheduler) GetCache() *CacheData {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	cacheCopy := *s.cache
	return &cacheCopy
}

func flattenCookies(cookiesByDomain map[string]map[string]string) map[string]string {
	flattened := make(map[string]string)
	for _, cookies := range cookiesByDomain {
		for name, value := range cookies {
			flattened[name] = value
		}
	}
	return flattened
}
