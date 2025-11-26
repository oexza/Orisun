package com.orisunlabs.orisun.client;

/**
 * Logger interface for the Orisun client
 */
public interface Logger {
    void debug(String message, Object... args);
    void info(String message, Object... args);
    void warn(String message, Object... args);
    void error(String message, Object... args);
    void error(String message, Throwable throwable, Object... args);
    
    /**
     * Check if debug logging is enabled
     * @return true if debug logging is enabled
     */
    boolean isDebugEnabled();
    
    /**
     * Check if info logging is enabled
     * @return true if info logging is enabled
     */
    boolean isInfoEnabled();
}