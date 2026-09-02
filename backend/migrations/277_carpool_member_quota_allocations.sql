ALTER TABLE carpool_members
    ADD COLUMN IF NOT EXISTS quota_share_ratio NUMERIC(12, 8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS weekly_limit_usd NUMERIC(18, 6) NOT NULL DEFAULT 0;

-- [MariaDB] data-modifying CTE rewritten: INNER JOIN derived table
UPDATE carpool_members m
INNER JOIN (
    SELECT
        m2.id AS member_id,
        CASE
            WHEN p.target_seats > 0 THEN 1.0 / p.target_seats
            WHEN ac.active_count > 0 THEN 1.0 / ac.active_count
            ELSE 0
        END AS share_ratio,
        CASE
            WHEN p.total_five_hour_limit_usd > 0 THEN p.total_five_hour_limit_usd
            WHEN p.per_member_five_hour_limit_usd > 0 THEN p.per_member_five_hour_limit_usd * COALESCE(NULLIF(p.target_seats, 0), ac.active_count)
            ELSE 0
        END AS total_five_hour_limit_usd,
        CASE
            WHEN p.total_weekly_limit_usd > 0 THEN p.total_weekly_limit_usd
            WHEN p.per_member_weekly_limit_usd > 0 THEN p.per_member_weekly_limit_usd * COALESCE(NULLIF(p.target_seats, 0), ac.active_count)
            ELSE 0
        END AS total_weekly_limit_usd
    FROM carpool_members m2
    INNER JOIN carpool_pools p ON p.id = m2.pool_id
    LEFT JOIN (
        SELECT pool_id, COUNT(*) AS active_count
        FROM carpool_members
        WHERE deleted_at IS NULL AND status = 'active'
        GROUP BY pool_id
    ) ac ON ac.pool_id = m2.pool_id
    WHERE m2.deleted_at IS NULL
) d ON m.id = d.member_id
SET
    m.quota_share_ratio = CASE WHEN m.quota_share_ratio > 0 THEN m.quota_share_ratio ELSE d.share_ratio END,
    m.five_hour_limit_usd = CASE WHEN m.five_hour_limit_usd > 0 THEN m.five_hour_limit_usd ELSE d.total_five_hour_limit_usd * d.share_ratio END,
    m.weekly_limit_usd = CASE WHEN m.weekly_limit_usd > 0 THEN m.weekly_limit_usd ELSE d.total_weekly_limit_usd * d.share_ratio END,
    m.updated_at = CURRENT_TIMESTAMP(6);