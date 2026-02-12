-- Swap verification tier values: vouch is now step 1 (tier 1), postcard is step 2 (tier 2)
-- Temporary value of 99 prevents constraint violations during swap
UPDATE users SET verification_tier = 99 WHERE verification_tier = 1;  -- old postcard → temp
UPDATE users SET verification_tier = 1 WHERE verification_tier = 2;   -- old vouch → 1
UPDATE users SET verification_tier = 2 WHERE verification_tier = 99;  -- old postcard → 2
