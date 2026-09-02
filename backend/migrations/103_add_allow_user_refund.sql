ALTER TABLE payment_provider_instances ADD COLUMN IF NOT EXISTS allow_user_refund TINYINT(1) NOT NULL DEFAULT false;
