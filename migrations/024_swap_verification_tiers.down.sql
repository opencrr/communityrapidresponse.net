-- Reverse: swap back to original values (postcard=1, vouch=2)
UPDATE users SET verification_tier = 99 WHERE verification_tier = 2;  -- new postcard → temp
UPDATE users SET verification_tier = 2 WHERE verification_tier = 1;   -- new vouch → 2
UPDATE users SET verification_tier = 1 WHERE verification_tier = 99;  -- new postcard → 1
