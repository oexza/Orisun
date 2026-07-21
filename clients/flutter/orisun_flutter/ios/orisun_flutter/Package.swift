// swift-tools-version: 5.9

import PackageDescription

let package = Package(
    name: "orisun_flutter",
    platforms: [
        .iOS("13.0")
    ],
    products: [
        .library(name: "orisun-flutter", targets: ["orisun_flutter"])
    ],
    dependencies: [
        .package(name: "FlutterFramework", path: "../FlutterFramework")
    ],
    targets: [
        .binaryTarget(
            name: "OrisunFlutterMobile",
            path: "Frameworks/OrisunFlutterMobile.xcframework"
        ),
        .target(
            name: "orisun_flutter",
            dependencies: [
                .product(name: "FlutterFramework", package: "FlutterFramework"),
                "OrisunFlutterMobile"
            ],
            path: "Sources/orisun_flutter",
            publicHeadersPath: ".",
            linkerSettings: [
                .linkedLibrary("resolv")
            ]
        )
    ]
)
