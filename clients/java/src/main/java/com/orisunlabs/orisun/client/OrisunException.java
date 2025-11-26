package com.orisunlabs.orisun.client;

import java.util.HashMap;
import java.util.Map;

public class OrisunException extends RuntimeException {
    private final Map<String, Object> context = new HashMap<>();
    
    public OrisunException(String message) {
        super(message);
    }

    public OrisunException(String message, Throwable cause) {
        super(message, cause);
    }
    
    public OrisunException(String message, Throwable cause, Map<String, Object> context) {
        super(message, cause);
        if (context != null) {
            this.context.putAll(context);
        }
    }
    
    /**
     * Add context information to the exception
     * @param key The context key
     * @param value The context value
     * @return This exception for method chaining
     */
    public OrisunException addContext(String key, Object value) {
        context.put(key, value);
        return this;
    }
    
    /**
     * Get context information
     * @param key The context key
     * @return The context value, or null if not found
     */
    public Object getContext(String key) {
        return context.get(key);
    }
    
    /**
     * Get all context information
     * @return Immutable copy of the context map
     */
    public Map<String, Object> getAllContext() {
        return new HashMap<>(context);
    }
    
    /**
     * Check if context contains a specific key
     * @param key The context key
     * @return true if the key exists in context
     */
    public boolean hasContext(String key) {
        return context.containsKey(key);
    }
    
    @Override
    public String getMessage() {
        StringBuilder sb = new StringBuilder(super.getMessage());
        
        if (!context.isEmpty()) {
            sb.append(" [Context: ");
            context.forEach((key, value) ->
                sb.append(key).append("=").append(value).append(", "));
            sb.setLength(sb.length() - 2); // Remove trailing ", "
            sb.append("]");
        }
        
        return sb.toString();
    }
}
