# Orisun Flutter example

Build the native libraries before running the example. For Android or iOS:

```bash
../../../../scripts/build_mobile.sh
../../../../scripts/prepare_flutter_mobile.sh
flutter run -d <device>
```

macOS applications are universal by default:

```bash
../../../../scripts/build_desktop.sh darwin arm64
../../../../scripts/build_desktop.sh darwin amd64
flutter run -d macos
```

The app stores its `local` boundary below the platform application-support
directory. Press **Append event** to commit locally and observe the event arrive
through an Orisun subscription stream.
