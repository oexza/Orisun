# Admin gRPC API Documentation

Orisun provides a comprehensive Admin gRPC service for user management and system administration. All Admin API calls require authentication using the same basic auth header as the EventStore API.

## Authentication

All Admin API calls require authentication:
```bash
# Default credentials: admin:changeit
# Authorization header: Basic YWRtaW46Y2hhbmdlaXQ=
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" ...
```

## Available Methods

### 1. CreateUser

Creates a new user with the specified details and roles.

**Request:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.Admin/CreateUser <<EOF
{
  "name": "John Doe",
  "username": "johndoe",
  "password": "securePassword123",
  "roles": ["admin"]
}
EOF
```

**Request Fields:**
- `name` (string, required): Full name of the user
- `username` (string, required): Unique username (min 3 characters)
- `password` (string, required): User password (min 6 characters)
- `roles` (array of strings, required): List of role assignments (at least one role required)

**Response:**
```json
{
  "user": {
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "John Doe",
    "username": "johndoe",
    "roles": ["admin"],
    "created_at": "2024-01-20T10:30:00Z"
  }
}
```

**Example: Create an admin user**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"name":"Admin User","username":"admin2","password":"adminPass123","roles":["admin"]}' \
  localhost:5005 orisun.Admin/CreateUser
```

**Example: Create a regular user**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"name":"Jane Smith","username":"janesmith","password":"userPass123","roles":["user"]}' \
  localhost:5005 orisun.Admin/CreateUser
```

---

### 2. DeleteUser

Deletes a user by their ID. Users cannot delete their own accounts.

**Request:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"user_id": "550e8400-e29b-41d4-a716-446655440000"}' \
  localhost:5005 orisun.Admin/DeleteUser
```

**Request Fields:**
- `user_id` (string, required): ID of the user to delete

**Response:**
```json
{
  "success": true
}
```

**Error Cases:**
- `INVALID_ARGUMENT`: user_id is empty
- `FAILED_PRECONDITION`: Attempting to delete your own account
- `NOT_FOUND`: User doesn't exist
- `FAILED_PRECONDITION`: User already deleted

**Example:**
```bash
# First list users to get the user_id
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  localhost:5005 orisun.Admin/ListUsers

# Then delete the user
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"user_id": "550e8400-e29b-41d4-a716-446655440000"}' \
  localhost:5005 orisun.Admin/DeleteUser
```

---

### 3. ChangePassword

Changes a user's password. Users can only change their own passwords.

**Request:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.Admin/ChangePassword <<EOF
{
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "current_password": "oldPassword123",
  "new_password": "newSecurePassword456"
}
EOF
```

**Request Fields:**
- `user_id` (string, required): ID of the user
- `current_password` (string, required): Current password for verification
- `new_password` (string, required): New password to set

**Response:**
```json
{
  "success": true
}
```

**Error Cases:**
- `INVALID_ARGUMENT`: Missing required fields
- `PERMISSION_DENIED`: Attempting to change another user's password
- `UNAUTHENTICATED`: Current password is incorrect

**Example:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"user_id":"550e8400-e29b-41d4-a716-446655440000","current_password":"oldPass","new_password":"newPass"}' \
  localhost:5005 orisun.Admin/ChangePassword
```

---

### 4. ListUsers

Lists all users in the system (excluding deleted users).

**Request:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  localhost:5005 orisun.Admin/ListUsers
```

**Response:**
```json
{
  "users": [
    {
      "user_id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "Admin User",
      "username": "admin",
      "roles": ["admin"],
      "created_at": "2024-01-01T00:00:00Z",
      "updated_at": "2024-01-20T10:30:00Z"
    },
    {
      "user_id": "550e8400-e29b-41d4-a716-446655440001",
      "name": "John Doe",
      "username": "johndoe",
      "roles": ["user"],
      "created_at": "2024-01-15T08:00:00Z",
      "updated_at": "2024-01-20T10:30:00Z"
    }
  ]
}
```

**Example:**
```bash
# List all users
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  localhost:5005 orisun.Admin/ListUsers

# Pretty print with jq
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  localhost:5005 orisun.Admin/ListUsers | jq
```

---

### 5. ValidateCredentials

Validates a username and password combination. Returns user details if valid.

**Request:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.Admin/ValidateCredentials <<EOF
{
  "username": "johndoe",
  "password": "userPass123"
}
EOF
```

**Request Fields:**
- `username` (string, required): Username to validate
- `password` (string, required): Password to validate

**Response (Success):**
```json
{
  "success": true,
  "user": {
    "user_id": "550e8400-e29b-41d4-a716-446655440001",
    "name": "John Doe",
    "username": "johndoe",
    "roles": ["user"],
    "created_at": "2024-01-15T08:00:00Z",
    "updated_at": "2024-01-20T10:30:00Z"
  }
}
```

**Response (Invalid Credentials):**
```json
{
  "success": false
}
```

**Example:**
```bash
# Valid credentials
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"username":"admin","password":"changeit"}' \
  localhost:5005 orisun.Admin/ValidateCredentials

# Invalid credentials (returns success: false)
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"username":"admin","password":"wrongpassword"}' \
  localhost:5005 orisun.Admin/ValidateCredentials
```

---

### 6. GetUserCount

Returns the total number of users in the system (including deleted users).

**Request:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  localhost:5005 orisun.Admin/GetUserCount
```

**Response:**
```json
{
  "count": 42
}
```

**Example:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  localhost:5005 orisun.Admin/GetUserCount
```

---

### 7. GetEventCount

Returns the total number of events in a specific boundary.

**Request:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"boundary": "orisun_test_1"}' \
  localhost:5005 orisun.Admin/GetEventCount
```

**Request Fields:**
- `boundary` (string, required): Boundary name to count events for

**Response:**
```json
{
  "count": 15000
}
```

**Example:**
```bash
# Count events in a boundary
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"boundary":"orisun_test_1"}' \
  localhost:5005 orisun.Admin/GetEventCount

# Count admin events
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"boundary":"orisun_admin"}' \
  localhost:5005 orisun.Admin/GetEventCount
```

---

### 8. CreateIndex

Creates a btree index on JSONB data fields for improved query performance. Indexes are created
concurrently and can be partial (filtered) or composite.

**Request:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.Admin/CreateIndex <<EOF
{
  "boundary": "orders",
  "name": "user_id",
  "fields": [
    {"json_key": "user_id", "value_type": "TEXT"}
  ]
}
EOF
```

**Request Fields:**
- `boundary` (string, required): Boundary name to create the index on
- `name` (string, required): Index name (will be prefixed as `{boundary}_{name}_idx`)
- `fields` (array of IndexField, required): Fields to include in the index
  - `json_key` (string): Key path in the JSONB data column
  - `value_type` (ValueType): Data type - TEXT, NUMERIC, BOOLEAN, or TIMESTAMPTZ
- `conditions` (array of IndexCondition, optional): Partial index filter conditions
  - `key` (string): JSONB key to filter on
  - `operator` (string): Comparison operator (=, >, <, >=, <=)
  - `value` (string): Value to compare against
- `condition_combinator` (ConditionCombinator): How to combine conditions - AND or OR (default: AND)

**Response:**
```json
{}
```

**Value Types:**
- `TEXT` (0): String values
- `NUMERIC` (1): Numeric values
- `BOOLEAN` (2): Boolean values
- `TIMESTAMPTZ` (3): Timestamp with timezone

**Examples:**

**Simple index on a text field:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"boundary":"orders","name":"user_id","fields":[{"json_key":"user_id","value_type":"TEXT"}]}' \
  localhost:5005 orisun.Admin/CreateIndex
```

**Composite index on multiple fields:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{
    "boundary": "orders",
    "name": "category_priority",
    "fields": [
      {"json_key": "category", "value_type": "TEXT"},
      {"json_key": "priority", "value_type": "TEXT"}
    ]
  }' \
  localhost:5005 orisun.Admin/CreateIndex
```

**Partial index (only index specific event types):**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{
    "boundary": "orders",
    "name": "placed_amount",
    "fields": [{"json_key": "amount", "value_type": "NUMERIC"}],
    "conditions": [{"key": "eventType", "operator": "=", "value": "OrderPlaced"}],
    "condition_combinator": "AND"
  }' \
  localhost:5005 orisun.Admin/CreateIndex
```

**Error Cases:**
- `INVALID_ARGUMENT`: Missing required fields, invalid index name
- `INVALID_ARGUMENT`: Invalid operator or combinator
- `NOT_FOUND`: Unknown boundary

**Notes:**
- Indexes are created with `CONCURRENTLY`, allowing continued database operations
- Index names are automatically prefixed: `{boundary}_{name}_idx`
- Use descriptive names to identify the indexed fields
- Partial indexes are more efficient for filtering specific event subsets

---

### 9. DropIndex

Drops an existing index created via CreateIndex.

**Request:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"boundary": "orders", "name": "user_id"}' \
  localhost:5005 orisun.Admin/DropIndex
```

**Request Fields:**
- `boundary` (string, required): Boundary name where the index exists
- `name` (string, required): Index name (without the `{boundary}_` prefix or `_idx` suffix)

**Response:**
```json
{}
```

**Example:**
```bash
# Drop the user_id index from orders boundary
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"boundary":"orders","name":"user_id"}' \
  localhost:5005 orisun.Admin/DropIndex
```

**Error Cases:**
- `INVALID_ARGUMENT`: Missing required fields
- `NOT_FOUND`: Unknown boundary or index doesn't exist

**Notes:**
- Indexes are dropped with `CONCURRENTLY`, allowing continued database operations
- If the index doesn't exist, the operation succeeds silently (IF EXISTS)

---

## Common Workflows

### Creating and Managing Users

```bash
# 1. Create a new user
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"name":"Jane Doe","username":"janedoe","password":"securePass123","roles":["user"]}' \
  localhost:5005 orisun.Admin/CreateUser

# 2. List all users to find the user_id
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  localhost:5005 orisun.Admin/ListUsers | jq

# 3. Validate credentials
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"username":"janedoe","password":"securePass123"}' \
  localhost:5005 orisun.Admin/ValidateCredentials

# 4. Change password
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"user_id":"<user_id_from_step_2>","current_password":"securePass123","new_password":"newSecurePass456"}' \
  localhost:5005 orisun.Admin/ChangePassword

# 5. Delete user (if needed)
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"user_id":"<user_id_from_step_2>"}' \
  localhost:5005 orisun.Admin/DeleteUser
```

### Monitoring System Statistics

```bash
# Get total user count
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  localhost:5005 orisun.Admin/GetUserCount

# Get event count per boundary
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"boundary":"orisun_test_1"}' \
  localhost:5005 orisun.Admin/GetEventCount

grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"boundary":"orisun_admin"}' \
  localhost:5005 orisun.Admin/GetEventCount
```

---

## Error Handling

All Admin API methods follow gRPC status codes:

- `OK`: Operation succeeded
- `INVALID_ARGUMENT`: Missing or invalid required fields
- `UNAUTHENTICATED`: Invalid credentials
- `PERMISSION_DENIED`: Insufficient permissions
- `NOT_FOUND`: Resource doesn't exist
- `FAILED_PRECONDITION`: Operation not allowed (e.g., deleting own account, user already deleted)
- `INTERNAL`: Server error (check logs)

---

## Notes

1. **Password Requirements**:
   - Minimum 6 characters
   - Stored securely using bcrypt hashing
   - Change password requires current password verification

2. **Username Requirements**:
   - Minimum 3 characters
   - Must be unique across the system

3. **Roles**:
   - At least one role must be specified
   - Common roles: `admin`, `user`
   - Custom roles can be created as needed

4. **User Deletion**:
   - Soft delete - users are marked as deleted but events remain
   - Deleted users are filtered from ListUsers results
   - Users cannot delete their own accounts

5. **Authentication**:
   - All methods require basic authentication
   - Default admin credentials: `admin:changeit`
   - Change default password immediately after first login

---

## Integration Examples

### Go Client

```go
import (
    "context"
    "log"

    "google.golang.org/grpc"
    "google.golang.org/grpc/metadata"
    pb "github.com/oexza/Orisun/orisun"
)

func main() {
    conn, err := grpc.Dial("localhost:5005", grpc.WithInsecure())
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()

    client := pb.NewAdminClient(conn)

    // Add authentication
    ctx := metadata.AppendToOutgoingContext(context.Background(),
        "authorization", "Basic YWRtaW46Y2hhbmdlaXQ=")

    // Create user
    resp, err := client.CreateUser(ctx, &pb.CreateUserRequest{
        Name:     "Jane Doe",
        Username: "janedoe",
        Password: "securePass123",
        Roles:    []string{"user"},
    })
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Created user: %s", resp.User.UserId)
}
```

### Node.js Client

```javascript
const grpc = require('@grpc/grpc-js');
const protoLoader = require('@grpc/proto-loader');
const fs = require('fs');

const packageDefinition = protoLoader.loadSync('orisun/admin.proto', {
    keepCase: true,
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true
});

const admin = grpc.loadPackageDefinition(packageDefinition).orisun;

const client = new admin.Admin('localhost:5005', grpc.credentials.createInsecure());

// Create metadata with authentication
const metadata = new grpc.Metadata();
metadata.add('authorization', 'Basic YWRtaW46Y2hhbmdlaXQ=');

// Create user
client.CreateUser({
    name: 'Jane Doe',
    username: 'janedoe',
    password: 'securePass123',
    roles: ['user']
}, metadata, (error, response) => {
    if (error) {
        console.error(error);
        return;
    }
    console.log('Created user:', response.user);
});
```

### Python Client

```python
import grpc
from orisun import admin_pb2, admin_pb2_grpc

# Create channel
channel = grpc.insecure_channel('localhost:5005')
client = admin_pb2_grpc.AdminClient(channel)

# Create metadata with authentication
metadata = (('authorization', 'Basic YWRtaW46Y2hhbmdlaXQ='),)

# Create user
try:
    response = client.CreateUser(
        admin_pb2.CreateUserRequest(
            name='Jane Doe',
            username='janedoe',
            password='securePass123',
            roles=['user']
        ),
        metadata=metadata
    )
    print(f"Created user: {response.user.user_id}")
except grpc.RpcError as e:
    print(f"Error: {e.code()}: {e.details()}")
```
