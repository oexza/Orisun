<?php

namespace Orisun\Client\Laravel;

use Illuminate\Support\ServiceProvider;
use Illuminate\Contracts\Foundation\Application;
use Orisun\Client\OrisunClient;
use Orisun\Client\Laravel\Commands\OrisunTestCommand;
use Psr\Log\LoggerInterface;

/**
 * Laravel Service Provider for Orisun Event Store
 */
class OrisunServiceProvider extends ServiceProvider
{
    /**
     * Register services.
     */
    public function register(): void
    {
        $this->mergeConfigFrom(
            __DIR__ . '/../../config/orisun.php',
            'orisun'
        );
        
        $this->app->singleton(OrisunClient::class, function (Application $app) {
            $config = $app['config']['orisun'];
            
            // Build target string - support multiple formats
            $target = $this->buildTarget($config);
            
            $options = [
                'boundary' => $config['boundary'],
                'tls' => $config['tls']['enabled'],
                'timeout' => $config['timeout'],
                'loadBalancingPolicy' => $config['load_balancing']['policy'],
                'keepaliveTimeMs' => $config['keepalive']['time_ms'],
                'keepaliveTimeoutMs' => $config['keepalive']['timeout_ms'],
                'keepalivePermitWithoutCalls' => $config['keepalive']['permit_without_calls'],
            ];
            
            $logger = $app->has(LoggerInterface::class) 
                ? $app->make(LoggerInterface::class)
                : null;
            
            return new OrisunClient($target, $options, $logger);
        });
        
        // Alias for easier dependency injection
        $this->app->alias(OrisunClient::class, 'orisun');
        
        // Register commands
        $this->commands([
            OrisunTestCommand::class,
        ]);
    }
    
    /**
     * Bootstrap services.
     */
    public function boot(): void
    {
        if ($this->app->runningInConsole()) {
            $this->publishes([
                __DIR__ . '/../../config/orisun.php' => config_path('orisun.php'),
            ], 'orisun-config');
            
            $this->publishes([
                __DIR__ . '/../../database/migrations' => database_path('migrations'),
            ], 'orisun-migrations');
        }
    }
    
    /**
     * Build target string from configuration
     * 
     * @param array $config Configuration array
     * @return string Target string
     */
    private function buildTarget(array $config): string
    {
        // Check if a full target is specified
        if (!empty($config['target'])) {
            return $config['target'];
        }
        
        // Check if multiple hosts are specified
        if (!empty($config['hosts'])) {
            if (is_array($config['hosts'])) {
                return implode(',', $config['hosts']);
            }
            return $config['hosts']; // Assume comma-separated string
        }
        
        // Default to single host:port
        return $config['host'] . ':' . $config['port'];
    }
    
    /**
     * Get the services provided by the provider.
     */
    public function provides(): array
    {
        return [
            OrisunClient::class,
            'orisun',
            OrisunTestCommand::class,
        ];
    }
}