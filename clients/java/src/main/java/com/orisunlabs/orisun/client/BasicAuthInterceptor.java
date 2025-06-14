package com.orisunlabs.orisun.client;

import io.grpc.*;
import java.util.Base64;

public class BasicAuthInterceptor implements ClientInterceptor {
    private final String credentials;

    public BasicAuthInterceptor(String username, String password) {
        this.credentials = "Basic " + Base64.getEncoder().encodeToString(
                (username + ":" + password).getBytes());
    }

    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
            MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
        
        return new ForwardingClientCall.SimpleForwardingClientCall<>(
                next.newCall(method, callOptions)) {

            @Override
            public void start(Listener<RespT> responseListener, Metadata headers) {
                // Add the authorization header
                headers.put(Metadata.Key.of("Authorization", Metadata.ASCII_STRING_MARSHALLER), credentials);
                super.start(responseListener, headers);
            }
        };
    }
}