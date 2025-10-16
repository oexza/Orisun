-- Script to set up PostgreSQL logical replication for event streaming
-- This script configures publications and replication slots for all event tables

-- Prerequisites (must be set in postgresql.conf):
-- wal_level = logical
-- max_replication_slots = 5
-- max_wal_senders = 10

-- Create a replication user if it doesn't exist
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'orisun_repl') THEN
        CREATE ROLE orisun_repl WITH LOGIN REPLICATION PASSWORD 'orisun_repl_password';
    END IF;
END
$$;

-- Grant necessary permissions to the replication user
GRANT SELECT ON ALL TABLES IN SCHEMA public TO orisun_repl;

-- Grant permissions for all boundary schemas
DO $$
DECLARE
    schema_rec RECORD;
BEGIN
    -- Loop through all schemas that have an events table
    FOR schema_rec IN 
        SELECT DISTINCT table_schema as schema_name
        FROM information_schema.tables 
        WHERE table_name = 'events' 
        AND table_schema NOT IN ('information_schema', 'pg_catalog', 'public')
    LOOP
        -- Grant SELECT permission on the events table
        EXECUTE format('GRANT SELECT ON %I.events TO orisun_repl', schema_rec.schema_name);
        RAISE NOTICE 'Granted SELECT permission on %.events to orisun_repl', schema_rec.schema_name;
    END LOOP;
END $$;

-- Drop existing publication if it exists
DROP PUBLICATION IF EXISTS orisun_events_pub;

-- Create publication for all event tables
DO $$
DECLARE
    schema_rec RECORD;
    table_list TEXT := '';
    first_table BOOLEAN := TRUE;
BEGIN
    -- Build the list of event tables across all schemas
    FOR schema_rec IN 
        SELECT DISTINCT table_schema as schema_name
        FROM information_schema.tables 
        WHERE table_name = 'events' 
        AND table_schema NOT IN ('information_schema', 'pg_catalog', 'public')
    LOOP
        IF NOT first_table THEN
            table_list := table_list || ', ';
        END IF;
        table_list := table_list || quote_ident(schema_rec.schema_name) || '.events';
        first_table := FALSE;
    END LOOP;
    
    -- Create the publication if we have tables
    IF table_list != '' THEN
        EXECUTE 'CREATE PUBLICATION orisun_events_pub FOR TABLE ' || table_list;
        RAISE NOTICE 'Created publication orisun_events_pub for tables: %', table_list;
    ELSE
        RAISE NOTICE 'No event tables found, skipping publication creation';
    END IF;
END $$;

-- Display current publications
SELECT pubname, puballtables, pubinsert, pubupdate, pubdelete, pubtruncate
FROM pg_publication 
WHERE pubname = 'orisun_events_pub';

-- Display tables in the publication
SELECT schemaname, tablename 
FROM pg_publication_tables 
WHERE pubname = 'orisun_events_pub'
ORDER BY schemaname, tablename;

-- Display current replication slots
SELECT slot_name, plugin, slot_type, database, active, restart_lsn, confirmed_flush_lsn 
FROM pg_replication_slots
WHERE slot_name LIKE 'orisun%';

-- Instructions for creating replication slot (done by the application)
-- The logical replication listener will create a temporary replication slot named 'orisun_events_slot'
-- when it starts up. This is handled automatically by the Go application.

RAISE NOTICE 'Logical replication setup complete!';
RAISE NOTICE 'Make sure your postgresql.conf has:';
RAISE NOTICE '  wal_level = logical';
RAISE NOTICE '  max_replication_slots = 5';
RAISE NOTICE '  max_wal_senders = 10';
RAISE NOTICE 'And restart PostgreSQL if you changed these settings.';