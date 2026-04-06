ALTER TABLE `groups`
    ADD COLUMN discoverable_by_unverified BOOLEAN NOT NULL DEFAULT FALSE
    AFTER trusted_vouch_threshold;
