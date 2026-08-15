-- Build the payment order uniqueness guarantee online.
-- MariaDB has no partial unique indexes. A generated column maps empty
-- out_trade_no to NULL so non-payment rows do not participate in the constraint.

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS out_trade_no_unique_key VARCHAR(255) AS (
        CASE WHEN out_trade_no <> '' THEN out_trade_no ELSE NULL END
    ) STORED;

CREATE UNIQUE INDEX IF NOT EXISTS paymentorder_out_trade_no_unique
    ON payment_orders (out_trade_no_unique_key);

DROP INDEX IF EXISTS paymentorder_out_trade_no ON payment_orders;
