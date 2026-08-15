-- 013: log orphan group_ids stored in users.allowed_groups (JSON array)
-- Purpose: record all references to groups that no longer exist before cleanup.

CREATE TABLE IF NOT EXISTS orphan_allowed_groups_audit (
    id          BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id     BIGINT NOT NULL,
    group_id    BIGINT NOT NULL,
    recorded_at DATETIME(6) NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, group_id)
);

-- Record orphan group_ids (present in users.allowed_groups but missing from groups).
INSERT IGNORE INTO orphan_allowed_groups_audit (user_id, group_id)
SELECT u.id, CAST(JSON_EXTRACT(u.allowed_groups, CONCAT('$[', n.idx, ']')) AS SIGNED)
FROM users u
JOIN (SELECT 0 AS idx UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9 UNION ALL SELECT 10 UNION ALL SELECT 11 UNION ALL SELECT 12 UNION ALL SELECT 13 UNION ALL SELECT 14 UNION ALL SELECT 15 UNION ALL SELECT 16 UNION ALL SELECT 17 UNION ALL SELECT 18 UNION ALL SELECT 19 UNION ALL SELECT 20 UNION ALL SELECT 21 UNION ALL SELECT 22 UNION ALL SELECT 23 UNION ALL SELECT 24 UNION ALL SELECT 25 UNION ALL SELECT 26 UNION ALL SELECT 27 UNION ALL SELECT 28 UNION ALL SELECT 29 UNION ALL SELECT 30 UNION ALL SELECT 31) n
  ON JSON_EXTRACT(u.allowed_groups, CONCAT('$[', n.idx, ']')) IS NOT NULL
LEFT JOIN groups g ON g.id = CAST(JSON_EXTRACT(u.allowed_groups, CONCAT('$[', n.idx, ']')) AS SIGNED)
WHERE u.allowed_groups IS NOT NULL
  AND g.id IS NULL;

CREATE INDEX IF NOT EXISTS idx_orphan_allowed_groups_audit_user_id
ON orphan_allowed_groups_audit(user_id);
