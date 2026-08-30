#!/bin/sh
set -eu

psql --set ON_ERROR_STOP=on --command \
  'CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())'

for file in /migrations/*.up.sql; do
  version=$(basename "$file")
  applied=$(psql --tuples-only --no-align --command \
    "SELECT 1 FROM schema_migrations WHERE version = '$version'")
  if [ "$applied" = "1" ]; then
    echo "skipping $version"
    continue
  fi

  echo "running $version"
  {
    echo 'BEGIN;'
    cat "$file"
    printf "\nINSERT INTO schema_migrations (version) VALUES ('%s');\n" "$version"
    echo 'COMMIT;'
  } | psql --set ON_ERROR_STOP=on
done
