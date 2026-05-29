---
title: Clients
description: Official client libraries and protobuf sources.
---

Orisun exposes gRPC services. You can use generated clients, `grpcurl`, or generate bindings for another language from the protobuf files.

## Official Clients

| Language | Package | Repository |
| --- | --- | --- |
| Go | `github.com/oexza/orisun-client-go` | [`orisun-client-go`](https://github.com/oexza/orisun-client-go) |
| Node.js | `@orisun/eventstore-client` | [`orisun-node-client`](https://github.com/oexza/orisun-node-client) |
| Java | `com.orisunlabs:orisun-java-client` | [`orisun-client-java`](https://github.com/oexza/orisun-client-java) |

## Install

```bash
go get github.com/oexza/orisun-client-go
npm install @orisun/eventstore-client
```

Use generated clients when you want typed request/response objects and subscription helpers. Use `grpcurl` for operational checks, debugging, and examples.

## Proto Files

The service definitions live in the main repository:

- [`proto/eventstore.proto`](https://github.com/oexza/Orisun/blob/main/proto/eventstore.proto)
- [`proto/admin.proto`](https://github.com/oexza/Orisun/blob/main/proto/admin.proto)

Generated Go bindings are kept in `orisun/`.

## Compatibility

Client libraries are generated from the public protobuf definitions. When upgrading Orisun, regenerate or update clients if the protobuf files changed.
