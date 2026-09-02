-- Migrate notification email lists from []string to []NotifyEmailEntry.
-- Old: ["a@x.com","b@x.com"]  New: [{"email":"a@x.com","disabled":false,"verified":false},...]
-- [MariaDB 重写] jsonb_array_elements_text + jsonb_agg + jsonb_build_object
--   -> JSON_TABLE 展开 + JSON_ARRAYAGG + JSON_OBJECT；首元素类型判断用 JSON_TYPE(JSON_EXTRACT(x,'$[0]'))。

-- 1) 用户余额通知邮箱
UPDATE users
SET balance_notify_extra_emails = (
    SELECT COALESCE(
        JSON_ARRAYAGG(JSON_OBJECT('email', jt.email, 'disabled', FALSE, 'verified', FALSE)),
        JSON_ARRAY()
    )
    FROM JSON_TABLE(
        (balance_notify_extra_emails),
        '$[*]' COLUMNS (email VARCHAR(320) PATH '$')
    ) AS jt
)
WHERE balance_notify_extra_emails IS NOT NULL
  AND balance_notify_extra_emails <> '[]'
  AND balance_notify_extra_emails <> ''
  AND JSON_VALID(balance_notify_extra_emails)
  AND JSON_LENGTH((balance_notify_extra_emails)) > 0
  AND JSON_TYPE(JSON_EXTRACT((balance_notify_extra_emails), '$[0]')) = 'STRING';

-- 2) 管理端配额通知邮箱
UPDATE settings
SET value = (
    SELECT COALESCE(
        JSON_ARRAYAGG(JSON_OBJECT('email', jt.email, 'disabled', FALSE, 'verified', FALSE)),
        JSON_ARRAY()
    )
    FROM JSON_TABLE(
        (value),
        '$[*]' COLUMNS (email VARCHAR(320) PATH '$')
    ) AS jt
)
WHERE `key` = 'account_quota_notify_emails'
  AND value IS NOT NULL
  AND value <> '[]'
  AND value <> ''
  AND JSON_VALID(value)
  AND JSON_LENGTH((value)) > 0
  AND JSON_TYPE(JSON_EXTRACT((value), '$[0]')) = 'STRING';
