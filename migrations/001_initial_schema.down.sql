-- Rollback Migration 001: Initial Schema
-- Drop tables in reverse order to handle foreign key constraints

DROP TABLE IF EXISTS email_notifications;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS blacklisted_users;
DROP TABLE IF EXISTS blacklist_votes;
DROP TABLE IF EXISTS blacklist_proposals;
DROP TABLE IF EXISTS invite_link_update_votes;
DROP TABLE IF EXISTS invite_link_update_proposals;
DROP TABLE IF EXISTS deletion_votes;
DROP TABLE IF EXISTS deletion_proposals;
DROP TABLE IF EXISTS signal_groups;
DROP TABLE IF EXISTS vouches;
DROP TABLE IF EXISTS verification_requests;
DROP TABLE IF EXISTS admin_boundaries;
DROP TABLE IF EXISTS user_regions;
DROP TABLE IF EXISTS geographic_regions;
DROP TABLE IF EXISTS users;
