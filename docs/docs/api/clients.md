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

## Authenticating from a client

Every call needs credentials. Send HTTP Basic in the `authorization` metadata header, or reuse a session token.

```bash
# Basic auth on every call
grpcurl -H 'Authorization: Basic YWRtaW46Y2hhbmdlaXQ=' localhost:5005 orisun.EventStore/Ping
```

Each authenticated response sets an `x-auth-token` header. A long-lived client can capture that token once and send it as `x-auth-token` on subsequent calls instead of re-sending Basic credentials; Orisun validates the token first and falls back to Basic. Read the full model in [Security & Authorization](../operations/security).

## Proto Files

The service definitions live in the main repository:

- [`proto/eventstore.proto`](https://github.com/oexza/Orisun/blob/main/proto/eventstore.proto)
- [`proto/admin.proto`](https://github.com/oexza/Orisun/blob/main/proto/admin.proto)

Generated Go bindings are kept in `orisun/`.

## Generate stubs for another language

If no official client exists for your language, generate stubs directly from the protobuf files with `protoc`. For example, for Python:

```bash
python -m grpc_tools.protoc \
  -I proto \
  --python_out=. --grpc_python_out=. \
  proto/eventstore.proto proto/admin.proto
```

Swap the `*_out` plugins for your target language. With gRPC reflection enabled (the default), you can also explore the API live:

```bash
grpcurl -H "$AUTH" localhost:5005 list
grpcurl -H "$AUTH" localhost:5005 describe orisun.EventStore
```

## Compatibility

Client libraries are generated from the public protobuf definitions. When upgrading Orisun, regenerate or update clients if the protobuf files changed.
