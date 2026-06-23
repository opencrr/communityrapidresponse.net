DROP TABLE IF EXISTS group_trust_vouches;
ALTER TABLE `groups` DROP COLUMN trusted_vouch_threshold;
ALTER TABLE signal_groups DROP COLUMN access_tier;
