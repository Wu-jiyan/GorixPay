-- 订单资金冻结：运营在订单列表冻结争议订单时，把该订单已入账的金额从
-- 商户可用余额挪到冻结余额（lib\Order::freeze 的等价能力）。
-- 订单本身仍是已支付状态，冻结只是账务动作，回调和退款不受影响。
ALTER TABLE payment_orders ADD COLUMN frozen_at DATETIME NULL;
CREATE INDEX idx_payment_orders_frozen_at ON payment_orders (frozen_at);