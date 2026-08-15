-- MariaDB: rename the unique index created by migration 120 to the canonical name.
ALTER TABLE payment_orders
    RENAME INDEX paymentorder_out_trade_no_unique TO paymentorder_out_trade_no;
