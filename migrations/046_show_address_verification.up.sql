ALTER TABLE `groups`
    ADD COLUMN show_address_verification BOOLEAN NOT NULL DEFAULT TRUE
    AFTER discoverable_by_unverified;
