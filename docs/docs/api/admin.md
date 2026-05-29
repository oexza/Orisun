---
title: Admin API
description: Manage users, credentials, and lightweight operational counts.
---

The Admin service handles users, credentials, and lightweight operational statistics. It does not manage event indexes; index management belongs to the EventStore service so embedded deployments can manage indexes without exposing Admin.

Admin is available in every server flavor:

- all-backends server
- PostgreSQL-only server
- SQLite-only server

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
  "roles": ["admin"]
}
EOF
```

Required fields:

| Field | Description |
| --- | --- |
| `name` | Human-readable name. |
| `username` | Unique login username. |
| `password` | Initial password. |
| `roles` | Role list, for example `["admin"]`. |

## ListUsers

```bash
grpcurl -H "$AUTH" localhost:5005 orisun.Admin/ListUsers
```

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

## GetEventCount

```bash
grpcurl -H "$AUTH" \
  -d '{"boundary":"orders"}' \
  localhost:5005 orisun.Admin/GetEventCount
```

## Proto source

The Admin protobuf source lives at [`proto/admin.proto`](https://github.com/oexza/Orisun/blob/main/proto/admin.proto).
