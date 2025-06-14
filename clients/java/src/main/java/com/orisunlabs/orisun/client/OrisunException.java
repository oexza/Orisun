package com.orisunlabs.orisun.client;

public class OrisunException extends RuntimeException {
    public OrisunException(String message) {
        super(message);
    }

    public OrisunException(String message, Throwable cause) {
        super(message, cause);
    }
}
