-- Rollback Migration 011: Audit log user agent
-- Remove user_agent column and indexes from audit_log

DROP INDEX IF EXISTS idx_audit_ip_created ON audit_log;
DROP INDEX IF EXISTS idx_audit_user_created ON audit_log;
DROP INDEX IF EXISTS idx_audit_resource ON audit_log;
DROP INDEX IF EXISTS idx_audit_action_created ON audit_log;

ALTER TABLE audit_log DROP COLUMN IF EXISTS user_agent;
