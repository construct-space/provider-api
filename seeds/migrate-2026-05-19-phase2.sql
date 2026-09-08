-- Phase 2 catch-up: ensure existing picker entries have a
-- route_via_operator set. AutoMigrate adds the column (default 'tank')
-- on insert, but existing rows pre-date the column and pick up the
-- type default (empty string). This script normalises them.
--
-- Safe to run multiple times.

UPDATE construct_picker_entries
SET route_via_operator = 'tank'
WHERE route_via_operator IS NULL OR route_via_operator = '';
