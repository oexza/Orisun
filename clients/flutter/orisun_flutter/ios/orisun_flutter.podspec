Pod::Spec.new do |s|
  s.name             = 'orisun_flutter'
  s.version          = '0.0.1'
  s.summary          = 'Embedded Orisun SQLite with CCC for Flutter.'
  s.description      = <<-DESC
Runs Orisun's NATS-free embedded SQLite event store inside native Flutter apps.
                       DESC
  s.homepage         = 'https://github.com/OrisunLabs/Orisun'
  s.license          = { :file => '../LICENSE' }
  s.author           = { 'Orisun Labs' => 'hello@orisunlabs.com' }
  s.source           = { :path => '.' }
  s.source_files     = 'orisun_flutter/Sources/orisun_flutter/**/*'
  s.dependency 'Flutter'
  s.platform         = :ios, '13.0'
  s.vendored_frameworks = 'orisun_flutter/Frameworks/OrisunFlutterMobile.xcframework'
  s.libraries = 'resolv'
  s.static_framework = true
  s.pod_target_xcconfig = {
    'DEFINES_MODULE' => 'YES',
    'EXCLUDED_ARCHS[sdk=iphonesimulator*]' => 'i386'
  }
end
