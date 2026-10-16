-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Every table of the public schema with its row count and a hash over its rows in text form,
-- one line per table. The Postgres upgrade tests (compose_pg_upgrade.sh, cnpg_pg_upgrade.sh)
-- compare it before and after the upgrade. Run with psql -At.
SELECT c.relname || ' ' || (xpath('/row/f/text()', query_to_xml(format(
    'SELECT count(*) || '' '' || md5(coalesce(string_agg(t::text, ''|'' ORDER BY t::text), '''')) AS f FROM public.%I t',
    c.relname), false, true, '')))[1]::text
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname = 'public' AND c.relkind = 'r'
  ORDER BY c.relname;
