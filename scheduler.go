package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	tls_client "github.com/bogdanfinn/tls-client"
)

type Scheduler struct {
	providers       []Provider
	refreshInterval time.Duration
	client          tls_client.HttpClient
	cache           *CacheData
	cacheMu         sync.RWMutex
	fetchLock       atomic.Bool
	ticker          *time.Ticker
	stopChan        chan struct{}
	stopOnce        sync.Once
}

func NewScheduler(refreshInterval time.Duration, providers []Provider, client tls_client.HttpClient) *Scheduler {
	return &Scheduler{
		providers:       providers,
		refreshInterval: refreshInterval,
		client:          client,
		cache: &CacheData{
			Zai:        &ProviderData{Status: "pending"},
			Kimi:       &ProviderData{Status: "pending"},
			Codex:      &ProviderData{Status: "pending"},
			Claude:     &ProviderData{Status: "pending"},
			OpenCodeGo: &ProviderData{Status: "pending"},
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

	for _, provider := range s.providers {
		if !provider.IsEnabled() {
			continue
		}

		data, err := provider.Fetch(s.client)
		resolved := s.resolveProviderResult(data, err, s.getProviderCache(provider.ID()))
		s.setProviderCache(provider.ID(), resolved)
	}

	s.cache.LastFetch = now.Format(time.RFC3339)
	s.cache.NextRefreshAt = now.Add(s.refreshInterval).Format(time.RFC3339)
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

	s.ticker = time.NewTicker(s.refreshInterval)
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
	cacheCopy.Zai = cloneProviderData(s.cache.Zai)
	cacheCopy.Kimi = cloneProviderData(s.cache.Kimi)
	cacheCopy.Codex = cloneProviderData(s.cache.Codex)
	cacheCopy.Claude = cloneProviderData(s.cache.Claude)
	cacheCopy.OpenCodeGo = cloneProviderData(s.cache.OpenCodeGo)
	return &cacheCopy
}

func (s *Scheduler) getProviderCache(id string) *ProviderData {
	switch id {
	case "zai":
		return s.cache.Zai
	case "kimi":
		return s.cache.Kimi
	case "codex":
		return s.cache.Codex
	case "claude":
		return s.cache.Claude
	case "opencodego":
		return s.cache.OpenCodeGo
	default:
		return nil
	}
}

func (s *Scheduler) setProviderCache(id string, data *ProviderData) {
	switch id {
	case "zai":
		s.cache.Zai = data
	case "kimi":
		s.cache.Kimi = data
	case "codex":
		s.cache.Codex = data
	case "claude":
		s.cache.Claude = data
	case "opencodego":
		s.cache.OpenCodeGo = data
	}
}

func cloneProviderData(data *ProviderData) *ProviderData {
	if data == nil {
		return nil
	}
	copy := *data
	if data.Session != nil {
		sessionCopy := *data.Session
		copy.Session = &sessionCopy
	}
	if data.Weekly != nil {
		weeklyCopy := *data.Weekly
		copy.Weekly = &weeklyCopy
	}
	if data.Monthly != nil {
		monthlyCopy := *data.Monthly
		copy.Monthly = &monthlyCopy
	}
	if data.Models != nil {
		modelsCopy := *data.Models
		copy.Models = &modelsCopy
	}
	if data.Credits != nil {
		creditsCopy := *data.Credits
		copy.Credits = &creditsCopy
	}
	return &copy
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
