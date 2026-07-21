# Orisun Embedded SQLite for Mobile

This package exposes Orisun's SQLite backend without starting a gRPC server,
NATS, JetStream, or any network listener. It is designed to be bound into
Android and iOS apps with `gomobile bind`.

SQLite remains the source of truth. CCC checks and writes execute atomically in
the same SQLite transaction. Live subscriptions catch up by position after an
in-process commit notification, with periodic polling as a no-miss fallback.

## Build

The repository pins `gomobile` and `gobind` as Go tool dependencies. On macOS
with Xcode and the Android SDK/NDK installed, initialize once and build both
artifacts from the repository root:

```bash
go tool gomobile init
./scripts/build_mobile.sh
```

This produces:

- `build/mobile/orisun-mobile.aar`, containing Java bindings and Android native libraries.
- `build/mobile/OrisunMobile.xcframework`, containing iOS device and simulator slices.
- `build/mobile/OrisunMobile.xcframework.zip`, suitable for release distribution or Swift Package Manager.
- `build/mobile/SHA256SUMS`, covering the distributable AAR and zipped XCFramework.

Android bindings use the `com.orisunlabs.orisun.mobile` Java package. Apple
bindings use the `Orisun` Objective-C prefix and the `OrisunMobile` framework
module.

Set `ANDROID_HOME` or `ANDROID_SDK_ROOT` if the SDK is not in the standard
macOS location. The defaults are Android API 24 and iOS 13.0; override them
with `ORISUN_MOBILE_ANDROID_API` and `ORISUN_MOBILE_IOS_VERSION`.
Release binaries are stripped with `-s -w` and built with `-trimpath`; set
`ORISUN_MOBILE_LDFLAGS` to override the linker flags for a diagnostic build.

The default AAR contains the two 64-bit targets used by current physical
devices and emulators (`android/arm64,android/amd64`). To include 32-bit ARM
and x86 as well, request gomobile's universal Android target explicitly:

```bash
ORISUN_MOBILE_ANDROID_TARGETS=android ./scripts/build_mobile.sh
```

Use `ORISUN_MOBILE_ANDROID_TARGETS=android/arm64` for the smallest artifact
when testing only on a physical ARM64 device. This setting changes only the
architectures packaged in the AAR, not Orisun behavior.

As a reference, the current stripped build is approximately 15 MB for the
default two-ABI AAR and 23 MB for the zipped XCFramework. These are archive
sizes rather than a stable size contract: the AAR carries both selected Android
ABIs, while the XCFramework carries iOS device and simulator slices. A current
ARM64 native slice is approximately 8 MB compressed. Toolchain and dependency
updates can change these figures.

The package's exported binding surface contains only strings, integers,
booleans, object handles, errors, and the `EventListener` callback interface.
Structured values cross the boundary as JSON.

Android/Kotlin opens the generated API through its stable package name:

```kotlin
import com.orisunlabs.orisun.mobile.Mobile

val store = Mobile.open(filesDir.absolutePath, """["mobile"]""")
val position = store.saveEvents("mobile", eventsJson, "", "")
store.close()
```

The XCFramework exposes Objective-C bindings that import into Swift:

```swift
import OrisunMobile

var bridgeError: NSError?
let store = OrisunMobileOpen(dataDirectory, #"["mobile"]"#, &bridgeError)
if let bridgeError { throw bridgeError }
defer { store?.close() }
```

## Open and close

```text
Open(appDataDirectory, `["accounts","payments"]`)
Store.Close()
```

Use an application-owned data directory. Each boundary creates
`{boundary}.db` and `{boundary}_metadata.db`, plus SQLite WAL files while open.
Keep the store open for the active application lifetime and close it during
normal application shutdown.

## Save

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

```text
Store.SaveEvents(boundary, eventsJSON, expectedPositionJSON, queryJSON)
```

The returned position has this shape:

```json
{"commit_position":1,"prepare_position":1}
```

For a CCC create-if-absent write, pass the not-exists position and the relevant
content query:

```json
{"commit_position":-1,"prepare_position":-1}
```

```json
{
  "criteria": [
    {"tags": [{"key": "accountId", "value": "account-1"}]}
  ]
}
```

Pass an empty string for both optional JSON arguments when no consistency
condition is required.

## Read

```text
Store.GetEvents(boundary, fromPositionJSON, queryJSON, count, descending)
Store.GetLatestByCriteria(boundary, queryJSON)
```

Event responses contain `data` and `metadata` as JSON values rather than
double-encoded JSON strings.

## Subscribe

Implement `EventListener`, then call:

```text
Store.Subscribe(boundary, subscriberName, afterPositionJSON, queryJSON, listener)
```

- An empty `afterPositionJSON` starts at the current end and delivers new events.
- The not-exists position replays from the beginning.
- `Subscription.Stop()` requests cancellation.
- `Subscription.IsDone()` reports when its worker has exited.

Callbacks run on a Go-managed worker thread. Dispatch UI changes to the
platform main thread. Persist the last successfully handled event position if
the app needs replay across restarts.

Subscription-name locks are process-local. Do not open the same boundary files
from multiple application processes and expect cross-process subscriber
exclusivity.
