<?php

return [
    /*
    |--------------------------------------------------------------------------
    | Orisun Event Store Configuration
    |--------------------------------------------------------------------------
    |
    | This file contains the configuration options for the Orisun Event Store
    | PHP client. You can configure the connection settings, timeouts, and
    | other client-specific options here.
    |
    */

    /*
    |--------------------------------------------------------------------------
    | Connection Settings
    |--------------------------------------------------------------------------
    |
    | Configure the connection to your Orisun EventStore instance.
    | You can use one of the following approaches:
    |
    | 1. Single host (default):
    |    'host' => 'localhost', 'port' => 50051
    |
    | 2. Multiple hosts for load balancing:
    |    'hosts' => ['host1:50051', 'host2:50051', 'host3:50051']
    |    or 'hosts' => 'host1:50051,host2:50051,host3:50051'
    |
    | 3. Full target string (DNS, IPv4, IPv6):
    |    'target' => 'dns:///eventstore.example.com:50051'
    |    'target' => 'ipv4:10.0.0.10:50051'
    |    'target' => 'ipv6:[::1]:50051'
    |
    */
    'host' => env('ORISUN_HOST', 'localhost'),
    'port' => env('ORISUN_PORT', 50051),
    
    // Multiple hosts for load balancing (comma-separated or array)
    'hosts' => env('ORISUN_HOSTS'),
    
    // Full target string (takes precedence over host/port and hosts)
    'target' => env('ORISUN_TARGET'),

    /*
    |--------------------------------------------------------------------------
    | TLS Configuration
    |--------------------------------------------------------------------------
    |
    | Configure TLS/SSL settings for secure connections.
    |
    */
    'tls' => [
        'enabled' => env('ORISUN_TLS_ENABLED', false),
        'cert_file' => env('ORISUN_TLS_CERT_FILE'),
        'key_file' => env('ORISUN_TLS_KEY_FILE'),
        'ca_file' => env('ORISUN_TLS_CA_FILE'),
    ],

    /*
    |--------------------------------------------------------------------------
    | Client Configuration
    |--------------------------------------------------------------------------
    |
    | Configure client-specific settings.
    |
    */
    'boundary' => env('ORISUN_BOUNDARY', 'default'),
    'timeout' => env('ORISUN_TIMEOUT', 30), // seconds

    /*
    |--------------------------------------------------------------------------
    | Load Balancing Configuration
    |--------------------------------------------------------------------------
    |
    | Configure client-side load balancing policies for distributing requests
    | across multiple server instances.
    |
    */
    'load_balancing' => [
        'policy' => env('ORISUN_LOAD_BALANCING_POLICY', 'round_robin'), // 'round_robin' or 'pick_first'
    ],

    /*
    |--------------------------------------------------------------------------
    | Keepalive Configuration
    |--------------------------------------------------------------------------
    |
    | Configure connection keepalive settings for better connection management
    | and health monitoring.
    |
    */
    'keepalive' => [
        'time_ms' => env('ORISUN_KEEPALIVE_TIME_MS', 30000), // Send keepalive ping every 30 seconds
        'timeout_ms' => env('ORISUN_KEEPALIVE_TIMEOUT_MS', 10000), // Wait 10 seconds for keepalive response
        'permit_without_calls' => env('ORISUN_KEEPALIVE_PERMIT_WITHOUT_CALLS', true), // Allow pings when no active calls
    ],

    /*
    |--------------------------------------------------------------------------
    | Default Stream Settings
    |--------------------------------------------------------------------------
    |
    | Configure default settings for stream operations.
    |
    */
    'defaults' => [
        'event_count' => env('ORISUN_DEFAULT_EVENT_COUNT', 100),
        'direction' => env('ORISUN_DEFAULT_DIRECTION', 'ASC'), // ASC or DESC
        'retry_attempts' => env('ORISUN_RETRY_ATTEMPTS', 3),
        'retry_delay' => env('ORISUN_RETRY_DELAY', 1000), // milliseconds
    ],

    /*
    |--------------------------------------------------------------------------
    | Logging Configuration
    |--------------------------------------------------------------------------
    |
    | Configure logging settings for the Orisun client.
    |
    */
    'logging' => [
        'enabled' => env('ORISUN_LOGGING_ENABLED', true),
        'level' => env('ORISUN_LOGGING_LEVEL', 'info'), // debug, info, warning, error
        'channel' => env('ORISUN_LOGGING_CHANNEL', 'default'),
    ],

    /*
    |--------------------------------------------------------------------------
    | Event Serialization
    |--------------------------------------------------------------------------
    |
    | Configure how events are serialized and deserialized.
    |
    */
    'serialization' => [
        'format' => env('ORISUN_SERIALIZATION_FORMAT', 'json'), // json, msgpack
        'compress' => env('ORISUN_SERIALIZATION_COMPRESS', false),
    ],

    /*
    |--------------------------------------------------------------------------
    | Health Check Configuration
    |--------------------------------------------------------------------------
    |
    | Configure health check settings for monitoring.
    |
    */
    'health_check' => [
        'enabled' => env('ORISUN_HEALTH_CHECK_ENABLED', true),
        'interval' => env('ORISUN_HEALTH_CHECK_INTERVAL', 60), // seconds
        'timeout' => env('ORISUN_HEALTH_CHECK_TIMEOUT', 5), // seconds
    ],
];