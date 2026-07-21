---
title: Flutter Embedding
description: Embed Orisun SQLite in native Flutter applications on Android, iOS, macOS, Windows, and Linux.
---

The `orisun_flutter` package runs Orisun's SQLite backend directly inside a
native Flutter application. It supports Android, iOS, macOS, Windows, and Linux
without a gRPC server, NATS, JetStream, or a network listener.

The runtime retains Orisun's SQLite storage and Command Context Consistency
semantics. Desktop calls a stable C ABI through FFI worker isolates. Android and
iOS call the same handle-based JSON protocol through a gomobile Flutter plugin
on a background platform task queue. Subscriptions are exposed as
`Stream<OrisunEvent>` everywhere.

## Platform support

| Platform | Architectures | Native artifact |
| --- | --- | --- |
| Android | ARM64 and x86-64 by default | `orisun-flutter-mobile.aar` |
| iOS | ARM64 device; ARM64 and x86-64 simulator | `OrisunFlutterMobile.xcframework` |
| macOS | ARM64 and x86-64 | `liborisun.dylib` |
| Linux | ARM64 and x86-64 | `liborisun.so` |
| Windows | ARM64 and x86-64 | `orisun.dll` |

The native library includes the Go runtime and SQLite implementation. A current
stripped desktop build is approximately 17–20 MB per architecture; the default
two-ABI Android AAR is about 15 MB and zipped iOS XCFramework about 23 MB. The
rest of a Flutter application's size comes from Flutter and application code.

## Build from this repository

Build Android and iOS on macOS with Xcode and the Android SDK/NDK:

```bash
go tool gomobile init
task build:mobile
./scripts/prepare_flutter_mobile.sh
```

Build the current host target:

```bash
task build:desktop
```

Or select a target explicitly:

```bash
./scripts/build_desktop.sh darwin arm64
./scripts/build_desktop.sh darwin amd64
./scripts/build_desktop.sh linux amd64
./scripts/build_desktop.sh windows amd64
```

The script writes under `build/desktop/<goos>-<goarch>/`. Flutter's native
assets hook finds those development builds automatically. A packaged Dart
release should place prebuilt libraries under
`clients/flutter/orisun_flutter/native/<goos>-<goarch>/` so application
developers do not need Go installed.

Assemble all native targets that are currently available into a distributable
Dart package archive with:

```bash
task package:flutter
# or choose an explicit release matrix
./scripts/package_flutter.sh \
  darwin-arm64 darwin-amd64 linux-amd64 windows-amd64
```

The archive includes `NATIVE_SHA256SUMS` and is written below `build/flutter/`.

Go `c-shared` mode requires a target C compiler. Build Linux and Windows
artifacts on native CI runners. A universal macOS application requires both
`darwin/arm64` and `darwin/amd64` libraries.

## Open and write

```dart
import 'package:orisun_flutter/orisun_flutter.dart';

final store = await OrisunStore.open(
  directory: applicationSupportDirectory,
  boundaries: const ['accounts'],
);

await store.saveEvents(
  'accounts',
  const [
    OrisunEventInput(
      id: '0198...',
      type: 'AccountOpened',
      data: {'accountId': 'account-1', 'owner': 'Ada'},
    ),
  ],
);
```

Use the platform application-support directory rather than a working-directory
relative path. Keep the store open for the application lifetime and close it
during normal shutdown.

## CCC writes

```dart
const context = OrisunQuery([
  OrisunCriterion([OrisunTag('accountId', 'account-1')]),
]);

try {
  await store.saveEvents(
    'accounts',
    events,
    expectedPosition: const OrisunPosition.notExists(),
    query: context,
  );
} on OrisunConflictException {
  // Reload the command context and decide again.
}
```

`getLatestByCriteria` returns aligned matches and a combined context position.
Pass that position to the next write using the same combined criteria.

## Reads and subscriptions

```dart
final events = await store.getEvents(
  'accounts',
  count: 100,
  direction: OrisunReadDirection.ascending,
);

final subscription = store
    .subscribe(
      'accounts',
      subscriberName: 'account-list',
      afterPosition: const OrisunPosition.notExists(),
    )
    .listen(updateProjection);
```

Omit `afterPosition` to begin at the current end. Use the not-exists position
to replay from the beginning. Persist the last successfully handled position
when replay must survive restarts.

The bridge queues native events until the Dart stream consumes
them. If the queue reaches 10,000 messages it reports an error instead of
growing without bound; restart from the last persisted position.

## Concurrency and process model

- Desktop calls are serialized through a worker isolate; Android and iOS calls
  run on a background platform task queue. SQLite controls transaction and read
  concurrency internally.
- Multiple Flutter windows in one process may share one store.
- Do not open the same boundary files from multiple application processes and
  expect subscription-name exclusivity. The local lock provider is
  process-scoped.
- Page large reads. JSON values cross the C/Dart boundary and very large pages
  require allocation and decoding on both sides.
- Include `{boundary}.db` and `{boundary}_metadata.db` in backup and migration
  policies.

The runnable example lives under
`clients/flutter/orisun_flutter/example` and uses the same Dart source on all
five native platforms.

Flutter web is not supported. Browsers cannot load the Go native runtime or
SQLite library; a web target would require a separate WASM/browser-storage
backend with an explicitly documented durability model.
