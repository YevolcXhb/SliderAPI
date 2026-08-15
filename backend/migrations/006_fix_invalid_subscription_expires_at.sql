-- Fix legacy subscription records with invalid expires_at (year > 2099).
UPDATE user_subscriptions
SET expires_at = '2099-12-31 23:59:59'
WHERE expires_at > '2099-12-31 23:59:59';
