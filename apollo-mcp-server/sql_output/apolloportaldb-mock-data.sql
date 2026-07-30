-- ===============================================
-- Apollo PortalDB - Mock 测试应用注册数据
-- ===============================================

Use ApolloPortalDB;

-- ---------- App 应用注册信息 ----------

-- 应用: user-service (用户服务)
INSERT INTO `App` (`AppId`, `Name`, `OrgId`, `OrgName`, `OwnerName`, `OwnerEmail`, `IsDeleted`, `DeletedAt`) VALUES ('user-service', '用户服务', 'org-001', '用户中心', 'admin', 'admin@company.com', b'0', 0);

-- 应用: order-service (订单服务)
INSERT INTO `App` (`AppId`, `Name`, `OrgId`, `OrgName`, `OwnerName`, `OwnerEmail`, `IsDeleted`, `DeletedAt`) VALUES ('order-service', '订单服务', 'org-002', '交易中心', 'admin', 'admin@company.com', b'0', 0);

-- 应用: payment-service (支付服务)
INSERT INTO `App` (`AppId`, `Name`, `OrgId`, `OrgName`, `OwnerName`, `OwnerEmail`, `IsDeleted`, `DeletedAt`) VALUES ('payment-service', '支付服务', 'org-003', '交易中心', 'admin', 'admin@company.com', b'0', 0);

-- 应用: rule-engine (规则引擎)
INSERT INTO `App` (`AppId`, `Name`, `OrgId`, `OrgName`, `OwnerName`, `OwnerEmail`, `IsDeleted`, `DeletedAt`) VALUES ('rule-engine', '规则引擎', 'org-004', '技术中台', 'admin', 'admin@company.com', b'0', 0);

-- 应用: notification-service (通知服务)
INSERT INTO `App` (`AppId`, `Name`, `OrgId`, `OrgName`, `OwnerName`, `OwnerEmail`, `IsDeleted`, `DeletedAt`) VALUES ('notification-service', '通知服务', 'org-005', '技术中台', 'admin', 'admin@company.com', b'0', 0);

-- 应用: api-gateway (API网关)
INSERT INTO `App` (`AppId`, `Name`, `OrgId`, `OrgName`, `OwnerName`, `OwnerEmail`, `IsDeleted`, `DeletedAt`) VALUES ('api-gateway', 'API网关', 'org-006', '技术中台', 'admin', 'admin@company.com', b'0', 0);
