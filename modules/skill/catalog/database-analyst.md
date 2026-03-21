---
name: database-analyst
description: Analyze database schema health, indexing gaps, and data quality issues
version: 1.0.0
author: cyntr
tools:
  - name: database_query
  - name: file_write
---

# Database Analyst

Perform comprehensive analysis of a SQLite or PostgreSQL database to identify
schema issues, missing indexes, data quality problems, and optimization
opportunities.

## Prerequisites

- A database connection must be available through the `database_query` tool.
- Determine the database type (SQLite or PostgreSQL) before proceeding, as
  queries differ between engines.

## Step 1: Identify Database Engine

Run a test query to determine the engine:

For SQLite detection:
```sql
SELECT sqlite_version();
```

For PostgreSQL detection:
```sql
SELECT version();
```

If both fail, inform the user that no supported database connection is available
and stop execution.

## Step 2: Enumerate Schema

### SQLite
```sql
SELECT name, type FROM sqlite_master WHERE type IN ('table', 'view') ORDER BY name;
```

### PostgreSQL
```sql
SELECT table_name, table_type
FROM information_schema.tables
WHERE table_schema = 'public'
ORDER BY table_name;
```

Record the full list of tables and views. Count each category.

## Step 3: Analyze Table Structure

For each table discovered in Step 2, retrieve column details:

### SQLite
```sql
PRAGMA table_info('<table_name>');
```

### PostgreSQL
```sql
SELECT column_name, data_type, is_nullable, column_default
FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = '<table_name>'
ORDER BY ordinal_position;
```

For each table, record:
- Column count
- Which columns allow NULLs
- Which columns have defaults
- Primary key columns

## Step 4: Index Analysis

### SQLite
```sql
SELECT name, tbl_name, sql FROM sqlite_master WHERE type = 'index' ORDER BY tbl_name;
```

### PostgreSQL
```sql
SELECT schemaname, tablename, indexname, indexdef
FROM pg_indexes
WHERE schemaname = 'public'
ORDER BY tablename, indexname;
```

For each table, check:
- Does it have a primary key? (Flag tables without one.)
- Are foreign key columns indexed? (Flag unindexed foreign keys.)
- Are there redundant indexes (indexes that are prefixes of other indexes)?

### PostgreSQL only — unused indexes:
```sql
SELECT schemaname, relname, indexrelname, idx_scan
FROM pg_stat_user_indexes
WHERE idx_scan = 0 AND schemaname = 'public'
ORDER BY relname;
```

Flag any index with zero scans as potentially unused.

## Step 5: Foreign Key Analysis

### SQLite
For each table:
```sql
PRAGMA foreign_key_list('<table_name>');
```

### PostgreSQL
```sql
SELECT
  tc.table_name, kcu.column_name,
  ccu.table_name AS foreign_table_name,
  ccu.column_name AS foreign_column_name
FROM information_schema.table_constraints AS tc
JOIN information_schema.key_column_usage AS kcu
  ON tc.constraint_name = kcu.constraint_name
JOIN information_schema.constraint_column_usage AS ccu
  ON ccu.constraint_name = tc.constraint_name
WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = 'public';
```

Also look for columns that appear to be foreign keys by naming convention
(ending in `_id`) but lack a formal foreign key constraint. Flag these as
"potential missing foreign keys."

## Step 6: Data Quality Checks

For each table (limit to tables with fewer than 1 million rows to avoid
expensive queries), run:

### Row count
```sql
SELECT COUNT(*) FROM <table_name>;
```

### NULL percentage per column
```sql
SELECT
  '<column>' AS col,
  COUNT(*) AS total,
  SUM(CASE WHEN <column> IS NULL THEN 1 ELSE 0 END) AS nulls,
  ROUND(100.0 * SUM(CASE WHEN <column> IS NULL THEN 1 ELSE 0 END) / COUNT(*), 2) AS null_pct
FROM <table_name>;
```

Flag any non-nullable-by-intent column with more than 10% NULLs.

### Duplicate detection
For tables with a natural key or unique columns:
```sql
SELECT <columns>, COUNT(*) AS cnt
FROM <table_name>
GROUP BY <columns>
HAVING COUNT(*) > 1
LIMIT 10;
```

### Orphaned records
For each foreign key relationship discovered in Step 5:
```sql
SELECT COUNT(*) FROM <child_table> c
LEFT JOIN <parent_table> p ON c.<fk_column> = p.<pk_column>
WHERE p.<pk_column> IS NULL;
```

Flag any orphaned record count greater than zero.

## Step 7: Table Size Analysis (PostgreSQL only)

```sql
SELECT
  relname AS table_name,
  pg_size_pretty(pg_total_relation_size(relid)) AS total_size,
  pg_size_pretty(pg_relation_size(relid)) AS data_size,
  pg_size_pretty(pg_total_relation_size(relid) - pg_relation_size(relid)) AS index_size
FROM pg_catalog.pg_statio_user_tables
ORDER BY pg_total_relation_size(relid) DESC;
```

## Step 8: Generate Report

Compile all findings into a structured report:

```
# Database Health Report
**Generated:** <current date>
**Engine:** <SQLite|PostgreSQL> <version>

## Schema Overview
- Tables: <count>
- Views: <count>
- Total columns: <count>
- Total indexes: <count>

## Issues Found

### Critical
<Tables without primary keys, orphaned records, potential data loss>

### Warning
<Missing indexes on foreign keys, high NULL percentages, unused indexes>

### Info
<Naming convention inconsistencies, potential missing foreign keys>

## Table Details
| Table | Rows | Columns | Indexes | Issues |
|-------|------|---------|---------|--------|
<rows>

## Data Quality Summary
| Table | NULL Issues | Duplicates | Orphans |
|-------|------------|------------|---------|
<rows>

## Optimization Recommendations
1. <Specific recommendation with SQL to implement>
2. <Next recommendation>
...
```

Prioritize recommendations by impact:
1. Add missing primary keys
2. Add indexes on frequently-joined foreign key columns
3. Add foreign key constraints on `_id` columns lacking them
4. Remove unused indexes (PostgreSQL)
5. Investigate and clean orphaned records
6. Address high NULL-percentage columns

If the user requested a file output, write the report using `file_write`.
Otherwise, return the report directly.
