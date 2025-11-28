package orisun

import (
	"encoding/base64"
	"sync"

	"google.golang.org/grpc/metadata"
)

// TokenCache for storing and managing authentication tokens
type TokenCache struct {
	cachedToken *string
	logger       Logger
	mu           sync.RWMutex
}

// NewTokenCache creates a new TokenCache with the given logger
func NewTokenCache(logger Logger) *TokenCache {
	if logger == nil {
		logger = NewNoOpLogger()
	}
	return &TokenCache{
		logger: logger,
	}
}

// CacheToken stores a token in the cache
func (tc *TokenCache) CacheToken(token string) {
	if token == "" {
		return
	}
	
	tc.mu.Lock()
	defer tc.mu.Unlock()
	
	tc.cachedToken = &token
	tc.logger.Debug("Cached authentication token")
}

// GetCachedToken returns the cached token, or empty string if no token is cached
func (tc *TokenCache) GetCachedToken() string {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	
	if tc.cachedToken == nil {
		return ""
	}
	return *tc.cachedToken
}

// HasToken returns true if a token is cached
func (tc *TokenCache) HasToken() bool {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	
	return tc.cachedToken != nil
}

// ClearToken clears the cached token
func (tc *TokenCache) ClearToken() {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	
	tc.cachedToken = nil
	tc.logger.Debug("Cleared cached authentication token")
}

// ExtractAndCacheToken extracts token from response metadata and caches it
func (tc *TokenCache) ExtractAndCacheToken(md metadata.MD) {
	if md == nil {
		return
	}
	
	values := md.Get("x-auth-token")
	if len(values) > 0 {
		token := values[0]
		if token != "" {
			tc.CacheToken(token)
			tc.logger.Debug("Extracted and cached token from response headers")
		}
	}
}

// CreateAuthMetadata creates metadata with authentication (cached token or basic auth)
func (tc *TokenCache) CreateAuthMetadata(basicAuthCredentials string) metadata.MD {
	md := metadata.New(nil)
	
	token := tc.GetCachedToken()
	if token != "" {
		md.Set("x-auth-token", token)
		tc.logger.Debug("Using cached authentication token")
	} else if basicAuthCredentials != "" {
		md.Set("Authorization", basicAuthCredentials)
		tc.logger.Debug("Using basic authentication")
	}
	
	return md
}

// CreateBasicAuthCredentials creates basic authentication credentials from username and password
func CreateBasicAuthCredentials(username, password string) string {
	if username == "" || password == "" {
		return ""
	}
	
	auth := username + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
}