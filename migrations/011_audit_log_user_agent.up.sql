-- Add user_agent column to audit_log table
ALTER TABLE audit_log ADD COLUMN user_agent VARCHAR(512) AFTER ip_address;

-- Add indexes for common query patterns
CREATE INDEX idx_audit_action_created ON audit_log(action, created_at);
CREATE INDEX idx_audit_resource ON audit_log(resource_type, resource_id);
CREATE INDEX idx_audit_user_created ON audit_log(user_id, created_at);
CREATE INDEX idx_audit_ip_created ON audit_log(ip_address, created_at);
