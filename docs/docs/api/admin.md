---
title: Admin API
description: Manage users, credentials, and lightweight operational counts.
---

The Admin service handles users, credentials, and lightweight operational statistics. It does not manage event indexes; index management belongs to the EventStore service so embedded deployments can manage indexes without exposing Admin.

Admin is available in every server flavor:

- PostgreSQL-compatible server
- SQLite-only server
- FoundationDB server

## Authentication

Admin calls use the same Basic auth header as EventStore calls:

```bash
AUTH='Authorization: Basic YWRtaW46Y2hhbmdlaXQ='
```

The default credentials are `admin:changeit`.

:::warning
Set a production password with `ORISUN_ADMIN_PASSWORD` before exposing the server.
:::

## Methods

| Method | Purpose |
| --- | --- |
| `CreateUser` | Create an admin or application user. |
| `DeleteUser` | Delete a user by ID. Users cannot delete their own account. |
| `ChangePassword` | Change the authenticated user's password. |
| `ListUsers` | List non-deleted users. |
| `ValidateCredentials` | Validate a username/password pair. |
| `GetUserCount` | Return total active user count. |
| `GetEventCount` | Return event count for a boundary. |

## CreateUser

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.Admin/CreateUser <<EOF
{
  "name": "Ops User",
  "username": "ops",
  "password": "change-this",
  "roles": ["OPERATIONS"]
}
EOF
```

Required fields:

| Field | Description |
| --- | --- |
| `name` | Human-readable name. |
| `username` | Unique login username. |
| `password` | Initial password. |
| `roles` | Role list. Valid values are `ADMIN` and `OPERATIONS`. |

:::warning
Roles are matched exactly and are case-sensitive. Use the uppercase values `ADMIN` and `OPERATIONS`; a value like `admin` is stored verbatim and never satisfies a role check, so the user is authenticated but authorized for nothing role-gated. See [Security & Authorization](../operations/security).
:::

## ListUsers

```bash
grpcurl -H "$AUTH" localhost:5005 orisun.Admin/ListUsers
```

## DeleteUser

```bash
grpcurl -H "$AUTH" \
  -d '{"user_id":"550e8400-e29b-41d4-a716-446655440000"}' \
  localhost:5005 orisun.Admin/DeleteUser
```

Authenticated users cannot delete their own account.

## ChangePassword

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.Admin/ChangePassword <<EOF
{
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "current_password": "changeit",
  "new_password": "replace-with-a-strong-password"
}
EOF
```

Users can only change their own password.

## ValidateCredentials

```bash
grpcurl -H "$AUTH" \
  -d '{"username":"ops","password":"change-this"}' \
  localhost:5005 orisun.Admin/ValidateCredentials
```

The response includes `success` and, when validation succeeds, the matching user.

## GetUserCount

```bash
grpcurl -H "$AUTH" localhost:5005 orisun.Admin/GetUserCount
```

## GetEventCount

```bash
grpcurl -H "$AUTH" \
  -d '{"boundary":"orders"}' \
  localhost:5005 orisun.Admin/GetEventCount
```

## Proto source

The Admin protobuf source lives at [`proto/admin.proto`](https://github.com/oexza/Orisun/blob/main/proto/admin.proto).
