-- Epay 参考库固定数据（Epay/install/install.sql 的 SQLite 子集）
--
-- 用途：tests/fixtures 里给迁移器提供一份可重复的“历史 Epay 库”。
-- 数据规模：2 商户 / 3 订单 / 1 退款 / 1 结算 / 1 代付 / 1 域名 / 1 黑名单。
--
-- 方言说明：
--   * install.sql 是 MySQL 方言。SQLite 没有 AUTO_INCREMENT，这里用 INTEGER
--     PRIMARY KEY 表达同样的自增语义。
--   * 金额列在 install.sql 里是 DECIMAL(10,2)。MySQL 的 DECIMAL 经 database/sql
--     读出就是字符串，SQLite 没有 DECIMAL，若声明成 NUMERIC 会被存成 REAL，
--     从而引入二进制浮点误差。为了让 fixture 与 MySQL 行为一致，金额列声明为
--     TEXT，值原样保留两位小数——列名与语义仍与 install.sql 完全一致。
--   * 时间列保持 DATETIME 声明，SQLite 以 TEXT 存储 "YYYY-MM-DD HH:MM:SS"。
--
-- 密码：Epay 的 pwd = md5(md5(明文) . md5('1277180438' . uid))，见
--       includes/common.php 的 getMd5Pwd()。这里的值由该公式算出：
--       uid=1001 明文 merchant1001，uid=1002 明文 merchant1002。
--       迁移后 password.VerifyLegacyMerchant 会用同样的公式校验并升级为 Argon2id。

DROP TABLE IF EXISTS pre_config;
CREATE TABLE pre_config (
  k VARCHAR(32) NOT NULL PRIMARY KEY,
  v TEXT
);
INSERT INTO pre_config (k, v) VALUES ('version', '2055');
INSERT INTO pre_config (k, v) VALUES ('sitename', 'Epay Fixture 支付平台');
INSERT INTO pre_config (k, v) VALUES ('title', 'Epay Fixture - 免签约支付');
INSERT INTO pre_config (k, v) VALUES ('settle_open', '1');
INSERT INTO pre_config (k, v) VALUES ('settle_type', '1');
INSERT INTO pre_config (k, v) VALUES ('user_refund', '1');
INSERT INTO pre_config (k, v) VALUES ('mail_pwd', 'smtp-secret-not-migrated');

DROP TABLE IF EXISTS pre_type;
CREATE TABLE pre_type (
  id INTEGER PRIMARY KEY,
  name VARCHAR(30) NOT NULL,
  device INTEGER NOT NULL DEFAULT 0,
  showname VARCHAR(30) NOT NULL,
  status INTEGER NOT NULL DEFAULT 0
);
INSERT INTO pre_type (id, name, device, showname, status) VALUES (1, 'alipay', 0, '支付宝', 1);
INSERT INTO pre_type (id, name, device, showname, status) VALUES (2, 'wxpay', 0, '微信支付', 1);

DROP TABLE IF EXISTS pre_user;
CREATE TABLE pre_user (
  uid INTEGER PRIMARY KEY,
  gid INTEGER NOT NULL DEFAULT 0,
  upid INTEGER NOT NULL DEFAULT 0,
  "key" VARCHAR(32) NOT NULL DEFAULT '',
  pwd VARCHAR(32),
  account VARCHAR(128),
  username VARCHAR(128),
  money TEXT NOT NULL DEFAULT '0.00',
  email VARCHAR(32),
  phone VARCHAR(20),
  addtime DATETIME,
  lasttime DATETIME,
  status INTEGER NOT NULL DEFAULT 0,
  pay INTEGER NOT NULL DEFAULT 1,
  settle INTEGER NOT NULL DEFAULT 1,
  refund INTEGER NOT NULL DEFAULT 1,
  cert INTEGER NOT NULL DEFAULT 0,
  deposit TEXT
);
INSERT INTO pre_user (uid, "key", pwd, account, username, money, email, phone, addtime, status, deposit)
VALUES (1001, 'apikey1001', 'e120f39b04b9814151ec35e721d94ed1', 'shop-a', '示例商户A', '135.79', 'shop@example.com', NULL, '2025-01-02 03:04:05', 1, '0.00');
INSERT INTO pre_user (uid, "key", pwd, account, username, money, email, phone, addtime, status, deposit)
VALUES (1002, 'apikey1002', 'cca810cdf3f9de23807c56e16a100e20', 'shop-b', NULL, '20.50', 'second@example.com', '13800000002', '2025-02-03 04:05:06', 0, NULL);

DROP TABLE IF EXISTS pre_order;
CREATE TABLE pre_order (
  trade_no VARCHAR(19) NOT NULL PRIMARY KEY,
  out_trade_no VARCHAR(150) NOT NULL,
  api_trade_no VARCHAR(150),
  uid INTEGER NOT NULL,
  tid INTEGER NOT NULL DEFAULT 0,
  type INTEGER NOT NULL DEFAULT 0,
  channel INTEGER NOT NULL DEFAULT 0,
  name VARCHAR(64) NOT NULL DEFAULT '',
  money TEXT NOT NULL DEFAULT '0.00',
  realmoney TEXT,
  getmoney TEXT,
  notify_url VARCHAR(255),
  return_url VARCHAR(255),
  param VARCHAR(255),
  addtime DATETIME NOT NULL,
  endtime DATETIME,
  ip VARCHAR(50),
  buyer VARCHAR(100),
  status INTEGER NOT NULL DEFAULT 0,
  notify INTEGER NOT NULL DEFAULT 0,
  settle INTEGER NOT NULL DEFAULT 0
);
INSERT INTO pre_order (trade_no, out_trade_no, api_trade_no, uid, type, channel, name, money, realmoney, getmoney, notify_url, return_url, param, addtime, endtime, ip, status)
VALUES ('20250102030405001', 'MCH-1001-0001', 'ALI-9990001', 1001, 1, 1, '订单A', '10.00', '10.00', '9.80', 'https://shop.example.com/notify', 'https://shop.example.com/return', 'uid=42', '2025-01-02 03:10:00', '2025-01-02 03:11:00', '203.0.113.5', 1);
INSERT INTO pre_order (trade_no, out_trade_no, api_trade_no, uid, type, channel, name, money, realmoney, getmoney, notify_url, return_url, param, addtime, endtime, ip, status)
VALUES ('20250102030405002', 'MCH-1001-0002', NULL, 1001, 2, 1, '订单B', '20.50', '20.50', NULL, '', NULL, NULL, '2025-01-02 04:10:00', NULL, '198.51.100.9', 0);
INSERT INTO pre_order (trade_no, out_trade_no, api_trade_no, uid, type, channel, name, money, realmoney, getmoney, notify_url, return_url, param, addtime, endtime, ip, status)
VALUES ('20250102030405003', 'MCH-1002-0001', 'WX-777', 1002, 1, 1, '订单C', '1.99', '1.99', '1.95', 'https://second.example.com/notify', NULL, NULL, '2025-02-03 04:10:00', '2025-02-03 04:20:00', '192.0.2.10', 2);

DROP TABLE IF EXISTS pre_refundorder;
CREATE TABLE pre_refundorder (
  refund_no VARCHAR(19) NOT NULL PRIMARY KEY,
  out_refund_no VARCHAR(150) NOT NULL,
  trade_no VARCHAR(19) NOT NULL,
  uid INTEGER NOT NULL DEFAULT 0,
  money TEXT NOT NULL DEFAULT '0.00',
  reducemoney TEXT,
  status INTEGER NOT NULL DEFAULT 0,
  addtime DATETIME,
  endtime DATETIME
);
INSERT INTO pre_refundorder (refund_no, out_refund_no, trade_no, uid, money, reducemoney, status, addtime, endtime)
VALUES ('R20250102030405001', 'MCH-1001-0001-R', '20250102030405001', 1001, '1.00', '1.00', 1, '2025-01-03 01:00:00', '2025-01-03 01:00:05');

DROP TABLE IF EXISTS pre_settle;
CREATE TABLE pre_settle (
  id INTEGER PRIMARY KEY,
  uid INTEGER NOT NULL,
  batch VARCHAR(20),
  auto INTEGER NOT NULL DEFAULT 1,
  type INTEGER NOT NULL DEFAULT 1,
  account VARCHAR(128) NOT NULL DEFAULT '',
  username VARCHAR(128) NOT NULL DEFAULT '',
  money TEXT NOT NULL DEFAULT '0.00',
  realmoney TEXT NOT NULL DEFAULT '0.00',
  addtime DATETIME,
  endtime DATETIME,
  status INTEGER NOT NULL DEFAULT 0,
  transfer_no VARCHAR(64),
  result VARCHAR(64)
);
INSERT INTO pre_settle (id, uid, batch, auto, type, account, username, money, realmoney, addtime, endtime, status, transfer_no, result)
VALUES (7, 1001, '20250103010000', 1, 1, 'shop@example.com', '示例商户A', '10.00', '9.90', '2025-01-03 01:10:00', '2025-01-03 02:00:00', 1, NULL, NULL);

DROP TABLE IF EXISTS pre_transfer;
CREATE TABLE pre_transfer (
  biz_no VARCHAR(19) NOT NULL PRIMARY KEY,
  out_biz_no VARCHAR(150) NOT NULL DEFAULT '',
  pay_order_no VARCHAR(80),
  uid INTEGER NOT NULL,
  type VARCHAR(10) NOT NULL DEFAULT '',
  channel INTEGER NOT NULL DEFAULT 0,
  account VARCHAR(128) NOT NULL DEFAULT '',
  username VARCHAR(128),
  money TEXT NOT NULL DEFAULT '0.00',
  costmoney TEXT,
  addtime DATETIME,
  paytime DATETIME,
  status INTEGER NOT NULL DEFAULT 0,
  api INTEGER NOT NULL DEFAULT 0,
  result VARCHAR(80)
);
INSERT INTO pre_transfer (biz_no, out_biz_no, uid, type, channel, account, username, money, costmoney, addtime, paytime, status, result)
VALUES ('T20250103010000001', 'MCH-1001-0001-T', 1001, 'alipay', 1, 'shop@example.com', '示例商户A', '5.00', '0.00', '2025-01-03 03:00:00', '2025-01-03 03:01:00', 1, 'SUCCESS');

DROP TABLE IF EXISTS pre_domain;
CREATE TABLE pre_domain (
  id INTEGER PRIMARY KEY,
  uid INTEGER NOT NULL DEFAULT 0,
  domain VARCHAR(128) NOT NULL,
  status INTEGER NOT NULL DEFAULT 0,
  addtime DATETIME,
  endtime DATETIME
);
INSERT INTO pre_domain (id, uid, domain, status, addtime, endtime)
VALUES (3, 1001, 'shop.example.com', 1, '2025-01-02 03:00:00', '2025-01-02 03:00:30');

DROP TABLE IF EXISTS pre_blacklist;
CREATE TABLE pre_blacklist (
  id INTEGER PRIMARY KEY,
  type INTEGER NOT NULL DEFAULT 0,
  content VARCHAR(50) NOT NULL,
  addtime DATETIME NOT NULL,
  endtime DATETIME,
  remark VARCHAR(80)
);
INSERT INTO pre_blacklist (id, type, content, addtime, endtime, remark)
VALUES (9, 1, '203.0.113.7', '2025-01-04 00:00:00', NULL, '批量注册');
