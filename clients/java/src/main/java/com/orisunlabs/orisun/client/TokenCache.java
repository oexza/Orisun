package com.orisunlabs.orisun.client;

import io.grpc.Metadata;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Token cache for storing and managing authentication tokens
 */
public class TokenCache {
    private final AtomicReference<String> cachedToken = new AtomicReference<>();
    private final Logger logger;
    
    public TokenCache(Logger logger) {
        this.logger = logger;
    }
    
    /**
     * Store a token in the cache
     * @param token The token to cache
     */
    public void cacheToken(String token) {
        if (token != null && !token.trim().isEmpty()) {
            cachedToken.set(token);
            logger.debug("Cached authentication token");
        }
    }
    
    /**
     * Get the cached token
     * @return The cached token, or null if no token is cached
     */
    public String getCachedToken() {
        return cachedToken.get();
    }
    
    /**
     * Check if a token is cached
     * @return true if a token is cached
     */
    public boolean hasToken() {
        return cachedToken.get() != null;
    }
    
    /**
     * Clear the cached token
     */
    public void clearToken() {
        cachedToken.set(null);
        logger.debug("Cleared cached authentication token");
    }
    
    /**
     * Extract token from response metadata and cache it
     * @param headers The response metadata
     */
    public void extractAndCacheToken(Metadata headers) {
        if (headers != null) {
            Metadata.Key<String> tokenKey = Metadata.Key.of("x-auth-token", Metadata.ASCII_STRING_MARSHALLER);
            String token = headers.get(tokenKey);
            
            if (token != null && !token.trim().isEmpty()) {
                cacheToken(token);
                logger.debug("Extracted and cached token from response headers");
            }
        }
    }
    
    /**
     * Create metadata with authentication (cached token or basic auth)
     * @param basicAuthCredentials Basic auth credentials to use if no token is cached
     * @return Metadata with authentication headers
     */
    public Metadata createAuthMetadata(String basicAuthCredentials) {
        Metadata metadata = new Metadata();
        
        String token = getCachedToken();
        if (token != null) {
            metadata.put(Metadata.Key.of("x-auth-token", Metadata.ASCII_STRING_MARSHALLER), token);
            logger.debug("Using cached authentication token");
        } else if (basicAuthCredentials != null) {
            metadata.put(Metadata.Key.of("Authorization", Metadata.ASCII_STRING_MARSHALLER), basicAuthCredentials);
            logger.debug("Using basic authentication");
        }
        
        return metadata;
    }
}