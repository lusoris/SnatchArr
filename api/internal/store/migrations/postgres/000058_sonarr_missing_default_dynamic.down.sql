-- Restore the column default for sonarr_missing_mode to 'episodes'.
ALTER TABLE snatch_policies
    ALTER COLUMN sonarr_missing_mode SET DEFAULT 'episodes';
