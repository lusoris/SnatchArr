-- Change the column default for sonarr_missing_mode from 'episodes' to
-- 'dynamic'.  Existing rows keep their stored value.
ALTER TABLE snatch_policies
    ALTER COLUMN sonarr_missing_mode SET DEFAULT 'dynamic';
