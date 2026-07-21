---
title: Mobile Embedding
description: Embed Orisun's NATS-free SQLite runtime in Android and iOS applications.
---

Orisun can run as an on-device event store inside Android and iOS applications.
The mobile runtime binds the SQLite backend directly into the application; it
does not start a gRPC server, NATS, JetStream, an admin service, or any network
listener.

SQLite remains the durable source of truth. Command Context Consistency checks
and event writes run atomically in the same SQLite transaction. Subscriptions
catch up from SQLite by position after an in-process commit notification and
periodically poll as a no-miss fallback.

## Choose the correct SQLite package

| Package | gRPC | NATS JetStream | Use case |
| --- | --- | --- | --- |
| `cmd/orisun-sqlite` | Yes | Yes | Standalone network server |
| `embedded/sqlite` | No | Yes | Embedded Go service with JetStream delivery |
| `embedded/sqlite/local` | No | No | Direct in-process Go or desktop application |
| `embedded/sqlite/mobile` | No | No | Bindable Android and iOS API |

The standalone SQLite server includes gRPC and NATS by design. Use the mobile
package when the event store belongs to one application process and no remote
clients need to connect.

## Build the platform libraries

The repository pins `gomobile` and `gobind` in `go.mod`. Building both
platforms requires macOS, Xcode, and an Android SDK with an NDK. From the
repository root:

```bash
go tool gomobile init
task build:mobile
```

The build writes:

| Path | Purpose |
| --- | --- |
| `build/mobile/orisun-mobile.aar` | Android library with Java bindings and native libraries |
| `build/mobile/OrisunMobile.xcframework` | Uncompressed Apple device and simulator framework |
| `build/mobile/OrisunMobile.xcframework.zip` | Distributable Apple framework archive |
| `build/mobile/orisun-flutter-mobile.aar` | Handle-based Android bridge used by `orisun_flutter` |
| `build/mobile/OrisunFlutterMobile.xcframework` | Handle-based iOS bridge used by `orisun_flutter` |
| `build/mobile/OrisunFlutterMobile.xcframework.zip` | Distributable Flutter iOS bridge archive |
| `build/mobile/SHA256SUMS` | SHA-256 checksums for all AAR and framework ZIP artifacts |

The generated files are build outputs and should not be committed to Git.
Produce them in release automation or on a trusted release machine.

### Build settings and size

Release builds use `-ldflags="-s -w"` and `-trimpath`. The default Android AAR
contains `android/arm64` for physical devices and `android/amd64` for x86-64
emulators. Select a different set with:

```bash
# Smallest artifact for testing on an ARM64 device
ORISUN_MOBILE_ANDROID_TARGETS=android/arm64 task build:mobile

# Universal AAR: arm, arm64, 386, and amd64
ORISUN_MOBILE_ANDROID_TARGETS=android task build:mobile
```

The build also accepts:

| Variable | Default | Purpose |
| --- | --- | --- |
| `ORISUN_MOBILE_ANDROID_API` | `24` | Minimum Android API used by gomobile |
| `ORISUN_MOBILE_ANDROID_TARGETS` | `android/arm64,android/amd64` | Native libraries packaged in the AAR |
| `ORISUN_MOBILE_IOS_VERSION` | `13.0` | Minimum iOS version |
| `ORISUN_MOBILE_LDFLAGS` | `-s -w` | Go linker flags; override for diagnostic builds |

As a reference, a stripped build from the current dependency graph is about
15 MB for the two-ABI Android AAR and 23 MB for the zipped XCFramework. These
are archive sizes, not a stable size guarantee: the AAR contains both Android
ABIs, and the XCFramework contains device and simulator slices. Toolchain and
dependency updates can change them.

## Android

Add `orisun-mobile.aar` as an Android library dependency. The generated API is
under `com.orisunlabs.orisun.mobile`:

```kotlin
import com.orisunlabs.orisun.mobile.Mobile

val boundaries = """["accounts"]"""
val store = Mobile.open(filesDir.absolutePath, boundaries)

try {
    val position = store.saveEvents(
        "accounts",
        eventsJson,
        "",
        "",
    )
} finally {
    store.close()
}
```

Use an application-owned directory such as `filesDir`; do not point multiple
processes at the same boundary files.

## iOS

Add `OrisunMobile.xcframework` to the Xcode project and import its module from
Swift:

```swift
import OrisunMobile

var bridgeError: NSError?
let store = OrisunMobileOpen(dataDirectory, #"["accounts"]"#, &bridgeError)
if let bridgeError { throw bridgeError }
defer { store?.close() }
```

The exported Objective-C API uses the `Orisun` prefix and imports as the
`OrisunMobile` module.

## JSON binding contract

The binding surface deliberately uses strings, integers, booleans, object
handles, errors, and one callback interface. Structured values cross the
language boundary as JSON.

Save an event array with:

```json
[
  {
    "event_id": "8e8135cc-3879-4bf7-93c4-06078c5a39ca",
    "event_type": "AccountOpened",
    "data": {"accountId": "account-1", "owner": "Ada"},
    "metadata": {"deviceId": "phone-1"}
  }
]
```

`SaveEvents` returns the committed position:

```json
{"commit_position": 1, "prepare_position": 1}
```

The mobile store exposes:

- `SaveEvents` for plain or CCC-checked writes,
- `GetEvents` for paged ascending or descending reads,
- `GetLatestByCriteria` for carried-state contexts,
- `Subscribe` for ordered local catch-up and live delivery,
- `CreateBoundaryIndex` and `DropBoundaryIndex` for SQLite expression indexes,
- `Close` for application shutdown.

Pass an empty expected-position and query string for a plain write. For a
create-if-absent CCC write, pass the not-exists position with the content query
that defines the command context:

```json
{"commit_position": -1, "prepare_position": -1}
```

```json
{
  "criteria": [
    {"tags": [{"key": "accountId", "value": "account-1"}]}
  ]
}
```

## Subscriptions and lifecycle

A blank subscription position starts at the current end and receives new
events. The not-exists position replays from the beginning. Persist the last
successfully handled event position when the application needs replay after a
restart.

Callbacks execute on a Go-managed worker thread. Dispatch UI changes to the
Android or iOS main thread. Keep the store open for the active application
lifetime, stop subscriptions when their owner is disposed, and close the store
during normal shutdown.

Each boundary creates `{boundary}.db` and `{boundary}_metadata.db`, plus SQLite
WAL files while open. Subscription-name locks are process-local. This profile
is intended for a single application process and does not provide cross-process
subscriber exclusion, remote access, server authentication, or server
telemetry.

For the complete JSON shapes and callback details, see the package-level
[mobile integration reference](https://github.com/OrisunLabs/Orisun/tree/main/embedded/sqlite/mobile).

For Flutter applications on Android, iOS, macOS, Windows, and Linux, use the
single typed Dart package described in [Flutter Embedding](./flutter). It wraps
these gomobile artifacts on Android/iOS and uses FFI on desktop.
