# orisun_flutter

Embedded Orisun SQLite for native Flutter apps. The package runs Command Context
Consistency, event reads, indexes, and subscriptions directly inside the
application process without gRPC, NATS, JetStream, or a network listener.

Supported targets:

| Platform | Architectures | Native library |
| --- | --- | --- |
| Android | ARM64, x86-64 by default | gomobile AAR |
| iOS | ARM64 device; ARM64 and x86-64 simulator | gomobile XCFramework |
| macOS | ARM64, x86-64 | `liborisun.dylib` |
| Linux | ARM64, x86-64 | `liborisun.so` |
| Windows | ARM64, x86-64 | `orisun.dll` |

The public Dart API is identical on all five platforms. Desktop communicates
with a stable C ABI through `dart:ffi` worker isolates. Android and iOS use a
thin Flutter plugin backed by gomobile on a background platform task queue.
Subscriptions appear to the application as `Stream<OrisunEvent>`.

## Build native libraries

From the Orisun repository root, build the current platform:

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

`c-shared` builds require a C toolchain for the target. Produce Windows and
Linux libraries on their native CI runners for releases. Flutter's native
assets hook searches for libraries under the repository's
`build/desktop/<goos>-<goarch>/` directory and then under this package's
`native/<goos>-<goarch>/` directory. Set `ORISUN_FLUTTER_NATIVE_LIBRARY` to an
explicit library path when testing another layout.

A distributable package should contain the prebuilt libraries below `native/`
so application developers do not need Go installed.

Build and stage the Android and iOS plugin libraries on macOS:

```bash
go tool gomobile init
task build:mobile
./scripts/prepare_flutter_mobile.sh
```

The mobile libraries are staged under `android/libs/` and `ios/Frameworks/`.
They are ignored source-tree build outputs but are included by the package
assembler and release archive.

After building the desired targets, assemble a distributable package archive:

```bash
./scripts/package_flutter.sh \
  darwin-arm64 darwin-amd64 linux-amd64 windows-amd64
```

With no target arguments, the script packages every desktop library currently present
under `build/desktop/`. It writes `build/flutter/orisun_flutter-<version>.tar.gz`
and adds the Android AAR and iOS XCFramework from `build/mobile/`. Every bundled
native checksum is recorded in `NATIVE_SHA256SUMS`.

macOS release applications are universal by default, so build both `darwin
arm64` and `darwin amd64` before running `flutter build macos`.

## Use the store

```dart
import 'package:orisun_flutter/orisun_flutter.dart';

final store = await OrisunStore.open(
  directory: applicationSupportDirectory,
  boundaries: const ['accounts'],
);

final position = await store.saveEvents(
  'accounts',
  const [
    OrisunEventInput(
      id: '0198...',
      type: 'AccountOpened',
      data: {'accountId': 'account-1', 'owner': 'Ada'},
    ),
  ],
);

final events = await store.getEvents('accounts');
await store.close();
```

## Command Context Consistency

```dart
const accountContext = OrisunQuery([
  OrisunCriterion([OrisunTag('accountId', 'account-1')]),
]);

try {
  await store.saveEvents(
    'accounts',
    events,
    expectedPosition: const OrisunPosition.notExists(),
    query: accountContext,
  );
} on OrisunConflictException {
  // Another matching event changed the command context.
}
```

Use the context position returned by `getLatestByCriteria` as the expected
position for the next write using the same combined criteria.

## Subscriptions

```dart
final subscription = store
    .subscribe(
      'accounts',
      subscriberName: 'account-list',
      afterPosition: const OrisunPosition.notExists(),
    )
    .listen(updateProjection);

await subscription.cancel();
```

A missing `afterPosition` starts at the current end. `OrisunPosition.notExists`
replays from the beginning. Persist the last successfully processed position
when replay must survive application restarts.

Native subscription messages are queued while Dart is busy. The bridge reports
an error and requires a restart from the last persisted position if the queue
reaches 10,000 messages; continuously consume streams rather than leaving them
unlistened.

## Process and lifecycle rules

- Keep one `OrisunStore` open for the active application lifetime.
- Close the store during normal application shutdown.
- Multiple windows in one process can share a store.
- Do not open the same boundary directory from separate processes and expect
  subscription-name exclusivity; local locks are process-scoped.
- Use an application-owned support directory and include both
  `{boundary}.db` and `{boundary}_metadata.db` in backup policies.
- Page large reads instead of requesting thousands of events per frame.

See `example/` for the runnable application on Android, iOS, macOS, Linux, and
Windows. Web is intentionally unsupported: browsers cannot load the native Go
runtime or SQLite library, so web would require a separate WASM/browser-storage
backend rather than pretending to provide the same durability contract.
