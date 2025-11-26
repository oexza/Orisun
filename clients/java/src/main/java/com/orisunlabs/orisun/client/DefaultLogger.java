package com.orisunlabs.orisun.client;

import java.time.LocalDateTime;
import java.time.format.DateTimeFormatter;

/**
 * Default logger implementation that outputs to console
 */
public class DefaultLogger implements Logger {
    private final LogLevel level;
    private final DateTimeFormatter formatter = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss.SSS");
    
    public enum LogLevel {
        DEBUG(0), INFO(1), WARN(2), ERROR(3);
        
        private final int value;
        
        LogLevel(int value) {
            this.value = value;
        }
        
        public int getValue() {
            return value;
        }
        
        public boolean isEnabled(LogLevel otherLevel) {
            return this.value <= otherLevel.getValue();
        }
    }
    
    public DefaultLogger(LogLevel level) {
        this.level = level;
    }
    
    public DefaultLogger() {
        this(LogLevel.INFO);
    }
    
    @Override
    public void debug(String message, Object... args) {
        if (isDebugEnabled()) {
            log("DEBUG", message, null, args);
        }
    }
    
    @Override
    public void info(String message, Object... args) {
        if (isInfoEnabled()) {
            log("INFO", message, null, args);
        }
    }
    
    @Override
    public void warn(String message, Object... args) {
        log("WARN", message, null, args);
    }
    
    @Override
    public void error(String message, Object... args) {
        error(message, null, args);
    }
    
    @Override
    public void error(String message, Throwable throwable, Object... args) {
        log("ERROR", message, throwable, args);
    }
    
    @Override
    public boolean isDebugEnabled() {
        return level.isEnabled(LogLevel.DEBUG);
    }
    
    @Override
    public boolean isInfoEnabled() {
        return level.isEnabled(LogLevel.INFO);
    }
    
    private void log(String level, String message, Throwable throwable, Object... args) {
        String timestamp = LocalDateTime.now().format(formatter);
        String formattedMessage = args.length > 0 ? String.format(message.replace("{}", "%s"), args) : message;
        
        String logLine = String.format("[%s] %s [%s] %s", timestamp, level, Thread.currentThread().getName(), formattedMessage);
        
        if (throwable != null) {
            System.err.println(logLine);
            throwable.printStackTrace();
        } else if ("ERROR".equals(level) || "WARN".equals(level)) {
            System.err.println(logLine);
        } else {
            System.out.println(logLine);
        }
    }
}