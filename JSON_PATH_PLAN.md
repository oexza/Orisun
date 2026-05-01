# JSON Path → Arrow Operator Plan

**Status**: Deferred — for future consideration

## Idea

Replace `Tag.key` (flat string) with `Tag.path` (dot-notation path like `"order.customer.id"`), and compile it in Go to PostgreSQL `->`/`->>` chains (`data->'order'->'customer'->>'id'`). No GIN dependency, backend-portable.

## Why

- Enables querying nested JSONB fields without schema changes
- `->`/`->>` expressions are btree-indexable — no conflict with the partial index strategy
- Compilation is a Go concern, so SQLite can compile the same path to `json_extract(data, '$.order.customer.id')`

## Changes

### 1. Proto — `proto/eventstore.proto`

```protobuf
message Tag {
  string path = 1;   // dot-notation: "order.customer.id"
  string value = 2;
}
```

### 2. Go — new path compiler

Shared utility function (in `postgres/` or new `internal/` package):

```go
// "order.customer.id" → "data->'order'->'customer'->>'id'"
func pathToSQL(path string) string
```

Logic: split on `.`, chain all segments with `->` except the last which uses `->>`.

### 3. Go — `postgres/postgres_eventstore.go`

**`getCriteriaAsList`** (line ~341): Change criteria JSON shape. Instead of `{"key": "value"}` maps, pass pre-compiled SQL fragments so PL/pgSQL doesn't need path awareness.

New shape: `{"path": "order.customer.id", "value": "abc", "sql": "data->'order'->'customer'->>'id'"}`

**`CreateBoundaryIndex`** (line ~647): Replace flat `data->>'key'` generation with `pathToSQL(f.JsonKey)`. Same for `IndexCondition.Key` in WHERE clause.

### 4. SQL — `postgres/scripts/common/db_scripts_1.sql`

Both PL/pgSQL loops in `get_matching_events_v3` and `insert_events_with_consistency_v3` change from:

```sql
FOR k, v IN SELECT * FROM jsonb_each_text(crit) LOOP
    crit_parts := crit_parts || format('(data->>%L = %L)', k, v);
END LOOP;
```

To consuming pre-compiled SQL fragments from the Go layer (the SQL just concatenates them with AND/OR).

### 5. Admin types — `admin/slices/common/common.go`

`IndexField.JsonKey` and `IndexCondition.Key` accept dot-delimited paths. `pathToSQL` handles compilation. Field names can stay the same — just the values change.

### 6. Client libraries (3 repos)

Regenerate from updated proto. `Tag.key` → `Tag.path`.

## Summary

| Layer | File | Change |
|---|---|---|
| Proto | `proto/eventstore.proto` | `Tag.key` → `Tag.path` |
| Go utility | new function | `pathToSQL("a.b.c")` → `data->'a'->'b'->>'c'` |
| Go PG layer | `postgres/postgres_eventstore.go` | `getCriteriaAsList` passes pre-compiled SQL; `CreateBoundaryIndex` uses `pathToSQL` |
| SQL functions | `db_scripts_1.sql` | Both PL/pgSQL loops consume pre-compiled fragments instead of `jsonb_each_text` |
| Admin types | `common/common.go` | `JsonKey`/`Key` values now accept dot paths |
| Clients | 3 repos | Regenerate from updated proto |

## Key Design Decision

Go pre-compiles paths to SQL fragments; PL/pgSQL stays dumb. This keeps compilation logic in one place and makes backend portability straightforward.