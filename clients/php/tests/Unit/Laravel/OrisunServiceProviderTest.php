<?php

namespace Orisun\Client\Tests\Unit\Laravel;

use PHPUnit\Framework\TestCase;
use Illuminate\Foundation\Application;
use Illuminate\Config\Repository as Config;
use Illuminate\Log\LogManager;
use Orisun\Client\Laravel\OrisunServiceProvider;
use Orisun\Client\Laravel\Commands\OrisunTestCommand;
use Orisun\Client\OrisunClient;
use Psr\Log\LoggerInterface;

class OrisunServiceProviderTest extends TestCase
{
    private Application $app;
    private OrisunServiceProvider $provider;

    protected function setUp(): void
    {
        $this->app = new Application();
        $this->app->instance('config', new Config([
            'orisun' => [
                'connection' => [
                    'host' => 'localhost',
                    'port' => 9090,
                ],
                'tls' => [
                    'enabled' => false,
                    'cert_file' => null,
                    'key_file' => null,
                    'ca_file' => null,
                ],
                'client' => [
                    'boundary' => 'orisun-boundary',
                    'timeout' => 30,
                ],
            ],
        ]));

        // Mock the log manager
        $logManager = $this->createMock(LogManager::class);
        $logger = $this->createMock(LoggerInterface::class);
        $logManager->method('channel')->willReturn($logger);
        $this->app->instance('log', $logManager);

        $this->provider = new OrisunServiceProvider($this->app);
    }

    public function testRegisterMergesConfiguration(): void
    {
        // Act
        $this->provider->register();

        // Assert - Check that config is accessible
        $config = $this->app['config'];
        $this->assertEquals('localhost', $config->get('orisun.connection.host'));
        $this->assertEquals(9090, $config->get('orisun.connection.port'));
        $this->assertFalse($config->get('orisun.tls.enabled'));
    }

    public function testRegisterBindsOrisunClientAsSingleton(): void
    {
        // Act
        $this->provider->register();

        // Assert - Check that OrisunClient is bound as singleton
        $this->assertTrue($this->app->bound(OrisunClient::class));
        $this->assertTrue($this->app->isShared(OrisunClient::class));
    }

    public function testRegisterCreatesOrisunClientWithCorrectConfiguration(): void
    {
        // Act
        $this->provider->register();
        
        // Get the binding closure
        $binding = $this->app->getBindings()[OrisunClient::class];
        $this->assertIsArray($binding);
        $this->assertTrue($binding['shared']); // Should be singleton
        
        // The actual client creation would require gRPC to be available
        // So we just verify the binding exists and is configured as singleton
        $this->assertArrayHasKey('concrete', $binding);
        $this->assertIsCallable($binding['concrete']);
    }

    public function testRegisterSetsUpAlias(): void
    {
        // Act
        $this->provider->register();

        // Assert - Check that alias is set up
        $aliases = $this->app->getAlias('orisun');
        $this->assertEquals(OrisunClient::class, $aliases);
    }

    public function testBootRegistersCommands(): void
    {
        // Arrange
        $this->app->instance('config', new Config([
            'app' => ['env' => 'testing']
        ]));
        
        // Mock the commands method
        $commandsCalled = false;
        $registeredCommands = [];
        
        // Override the commands method to capture what gets registered
        $provider = new class($this->app) extends OrisunServiceProvider {
            public $capturedCommands = [];
            
            protected function commands($commands): void
            {
                $this->capturedCommands = is_array($commands) ? $commands : [$commands];
            }
        };

        // Act
        $provider->boot();

        // Assert
        $this->assertContains(OrisunTestCommand::class, $provider->capturedCommands);
    }

    public function testBootPublishesConfiguration(): void
    {
        // Arrange
        $publishedPaths = [];
        
        // Override the publishes method to capture what gets published
        $provider = new class($this->app) extends OrisunServiceProvider {
            public $publishedPaths = [];
            
            protected function publishes(array $paths, $groups = null): void
            {
                $this->publishedPaths = array_merge($this->publishedPaths, $paths);
            }
        };

        // Act
        $provider->boot();

        // Assert
        $this->assertNotEmpty($provider->publishedPaths);
        
        // Check that the config file is being published to the right location
        $configPublished = false;
        foreach ($provider->publishedPaths as $source => $destination) {
            if (str_contains($destination, 'config/orisun.php')) {
                $configPublished = true;
                break;
            }
        }
        $this->assertTrue($configPublished, 'Configuration file should be published');
    }

    public function testProvidesReturnsCorrectServices(): void
    {
        // Act
        $provides = $this->provider->provides();

        // Assert
        $this->assertIsArray($provides);
        $this->assertContains(OrisunClient::class, $provides);
        $this->assertContains('orisun', $provides);
    }

    public function testIsDeferred(): void
    {
        // Act & Assert
        $this->assertFalse($this->provider->isDeferred());
    }

    public function testConfigurationWithTlsEnabled(): void
    {
        // Arrange
        $this->app->instance('config', new Config([
            'orisun' => [
                'connection' => [
                    'host' => 'secure.example.com',
                    'port' => 443,
                ],
                'tls' => [
                    'enabled' => true,
                    'cert_file' => '/path/to/cert.pem',
                    'key_file' => '/path/to/key.pem',
                    'ca_file' => '/path/to/ca.pem',
                ],
            ],
        ]));

        $provider = new OrisunServiceProvider($this->app);

        // Act
        $provider->register();

        // Assert
        $config = $this->app['config'];
        $this->assertEquals('secure.example.com', $config->get('orisun.connection.host'));
        $this->assertEquals(443, $config->get('orisun.connection.port'));
        $this->assertTrue($config->get('orisun.tls.enabled'));
        $this->assertEquals('/path/to/cert.pem', $config->get('orisun.tls.cert_file'));
    }

    public function testConfigurationWithCustomTimeout(): void
    {
        // Arrange
        $this->app->instance('config', new Config([
            'orisun' => [
                'connection' => [
                    'host' => 'localhost',
                    'port' => 9090,
                ],
                'client' => [
                    'timeout' => 60,
                    'boundary' => 'custom-boundary',
                ],
            ],
        ]));

        $provider = new OrisunServiceProvider($this->app);

        // Act
        $provider->register();

        // Assert
        $config = $this->app['config'];
        $this->assertEquals(60, $config->get('orisun.client.timeout'));
        $this->assertEquals('custom-boundary', $config->get('orisun.client.boundary'));
    }
}