-- Grant root access from any host (for Docker host connections during testing)
-- This runs before other migrations due to filename ordering

-- Allow root to connect from any host
CREATE USER IF NOT EXISTS 'root'@'%' IDENTIFIED BY 'testroot';
GRANT ALL PRIVILEGES ON *.* TO 'root'@'%' WITH GRANT OPTION;
FLUSH PRIVILEGES;
