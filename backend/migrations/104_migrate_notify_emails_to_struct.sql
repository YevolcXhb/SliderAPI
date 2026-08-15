-- Migrate notification email lists from old []string format to new []NotifyEmailEntry format
-- Old: ["a@x.com", "b@x.com"]
-- New: [{"email":"a@x.com","disabled":false,"verified":true}, ...]
-- Existing emails are marked as verified=false (unverified), disabled=false (enabled)

-- 1. User balance notification emails
UPDATE users
SET balance_notify_extra_emails = COALESCE(
    (
        SELECT JSON_ARRAYAGG(JSON_OBJECT('email', CAST(JSON_UNQUOTE(JSON_EXTRACT(u.balance_notify_extra_emails, CONCAT('$[', n.idx, ']'))) AS CHAR), 'disabled', false, 'verified', false))
        FROM users u
        JOIN (SELECT 0 AS idx UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3
              UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7
              UNION ALL SELECT 8 UNION ALL SELECT 9 UNION ALL SELECT 10 UNION ALL SELECT 11
              UNION ALL SELECT 12 UNION ALL SELECT 13 UNION ALL SELECT 14 UNION ALL SELECT 15
              UNION ALL SELECT 16 UNION ALL SELECT 17 UNION ALL SELECT 18 UNION ALL SELECT 19
              UNION ALL SELECT 20 UNION ALL SELECT 21 UNION ALL SELECT 22 UNION ALL SELECT 23
              UNION ALL SELECT 24 UNION ALL SELECT 25 UNION ALL SELECT 26 UNION ALL SELECT 27
              UNION ALL SELECT 28 UNION ALL SELECT 29 UNION ALL SELECT 30 UNION ALL SELECT 31) n
          ON JSON_EXTRACT(u.balance_notify_extra_emails, CONCAT('$[', n.idx, ']')) IS NOT NULL
        WHERE u.id = users.id
    ),
    '[]'
)
WHERE balance_notify_extra_emails IS NOT NULL
  AND balance_notify_extra_emails <> '[]'
  AND balance_notify_extra_emails <> ''
  AND JSON_EXTRACT(balance_notify_extra_emails, '$[0]') IS NOT NULL
  AND JSON_TYPE(JSON_EXTRACT(balance_notify_extra_emails, '$[0]')) = 'string';

-- 2. Admin account quota notification emails
UPDATE settings
SET value = COALESCE(
    (
        SELECT JSON_ARRAYAGG(JSON_OBJECT('email', CAST(JSON_UNQUOTE(JSON_EXTRACT(s.value, CONCAT('$[', n.idx, ']'))) AS CHAR), 'disabled', false, 'verified', false))
        FROM settings s
        JOIN (SELECT 0 AS idx UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3
              UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7
              UNION ALL SELECT 8 UNION ALL SELECT 9 UNION ALL SELECT 10 UNION ALL SELECT 11
              UNION ALL SELECT 12 UNION ALL SELECT 13 UNION ALL SELECT 14 UNION ALL SELECT 15
              UNION ALL SELECT 16 UNION ALL SELECT 17 UNION ALL SELECT 18 UNION ALL SELECT 19
              UNION ALL SELECT 20 UNION ALL SELECT 21 UNION ALL SELECT 22 UNION ALL SELECT 23
              UNION ALL SELECT 24 UNION ALL SELECT 25 UNION ALL SELECT 26 UNION ALL SELECT 27
              UNION ALL SELECT 28 UNION ALL SELECT 29 UNION ALL SELECT 30 UNION ALL SELECT 31) n
          ON JSON_EXTRACT(s.value, CONCAT('$[', n.idx, ']')) IS NOT NULL
        WHERE s.`key` = 'account_quota_notify_emails'
    ),
    '[]'
)
WHERE `key` = 'account_quota_notify_emails'
  AND value IS NOT NULL
  AND value <> '[]'
  AND value <> ''
  AND JSON_EXTRACT(value, '$[0]') IS NOT NULL
  AND JSON_TYPE(JSON_EXTRACT(value, '$[0]')) = 'string';
