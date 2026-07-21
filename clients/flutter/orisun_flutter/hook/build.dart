import 'dart:io';

import 'package:code_assets/code_assets.dart';
import 'package:hooks/hooks.dart';

void main(List<String> args) async {
  await build(args, (input, output) async {
    if (!input.config.buildCodeAssets) {
      return;
    }

    final targetOS = input.config.code.targetOS;
    // Android and iOS use the gomobile Flutter plugin bundled by Gradle/CocoaPods.
    if (targetOS == OS.android || targetOS == OS.iOS) {
      return;
    }
    final targetArchitecture = input.config.code.targetArchitecture;
    final target = _targetDirectory(targetOS, targetArchitecture);
    final libraryName = _libraryName(targetOS);
    final override = Platform.environment['ORISUN_FLUTTER_NATIVE_LIBRARY'];
    final candidates = <Uri>[
      if (override != null && override.isNotEmpty) Uri.file(override),
      input.packageRoot.resolve('native/$target/$libraryName'),
      input.packageRoot.resolve('../../../build/desktop/$target/$libraryName'),
    ];
    final library = candidates
        .where((uri) => File.fromUri(uri).existsSync())
        .firstOrNull;
    if (library == null) {
      throw StateError(
        'Orisun native library for $target was not found. Run '
        '`./scripts/build_desktop.sh ${_goOS(targetOS)} ${_goArchitecture(targetArchitecture)}` '
        'from the Orisun repository, bundle the result below native/$target, '
        'or set ORISUN_FLUTTER_NATIVE_LIBRARY. Checked: '
        '${candidates.map((uri) => uri.toFilePath()).join(', ')}',
      );
    }

    output.dependencies.add(library);
    output.assets.code.add(
      CodeAsset(
        package: input.packageName,
        name: 'orisun_flutter_bindings_generated.dart',
        linkMode: DynamicLoadingBundled(),
        file: library,
      ),
    );
  });
}

String _targetDirectory(OS os, Architecture architecture) =>
    '${_goOS(os)}-${_goArchitecture(architecture)}';

String _goOS(OS os) => switch (os) {
  OS.macOS => 'darwin',
  OS.linux => 'linux',
  OS.windows => 'windows',
  _ => throw UnsupportedError(
    'orisun_flutter currently supports macOS, Linux, and Windows desktop',
  ),
};

String _goArchitecture(Architecture architecture) => switch (architecture) {
  Architecture.arm64 => 'arm64',
  Architecture.x64 => 'amd64',
  _ => throw UnsupportedError(
    'orisun_flutter supports only ARM64 and x86-64 desktop targets',
  ),
};

String _libraryName(OS os) => switch (os) {
  OS.macOS => 'liborisun.dylib',
  OS.linux => 'liborisun.so',
  OS.windows => 'orisun.dll',
  _ => throw UnsupportedError(
    'orisun_flutter currently supports macOS, Linux, and Windows desktop',
  ),
};
