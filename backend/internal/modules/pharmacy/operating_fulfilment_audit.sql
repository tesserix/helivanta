SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE pharmacy_operating_stock_audit
 DROP CONSTRAINT pharmacy_operating_stock_audit_action_check,
 DROP CONSTRAINT pharmacy_operating_stock_audit_resource_kind_check,
 DROP CONSTRAINT pharmacy_operating_stock_audit_permission_check,
 DROP CONSTRAINT pharmacy_operating_stock_audit_check,
 ADD CONSTRAINT pharmacy_operating_stock_audit_action_check CHECK(action IN('pharmacy.operating_stock.read','pharmacy.operating_stock.batch_recorded','pharmacy.operating_stock.receipt_recorded','supplier_fulfilment_access')) NOT VALID,
 ADD CONSTRAINT pharmacy_operating_stock_audit_resource_kind_check CHECK(resource_kind IN('product','location','batch','receipt','supplier_order')) NOT VALID,
 ADD CONSTRAINT pharmacy_operating_stock_audit_permission_check CHECK(permission IN('pharmacy.stock.read','pharmacy.stock.receive','pharmacy.stock.fulfil')) NOT VALID,
 ADD CONSTRAINT pharmacy_operating_stock_audit_check CHECK(
  CASE WHEN action='supplier_fulfilment_access' THEN permission='pharmacy.stock.fulfil'AND resource_kind='supplier_order'AND resource_id IS NOT NULL
  ELSE resource_kind IN('product','location','batch','receipt')AND
   CASE action WHEN 'pharmacy.operating_stock.read' THEN permission='pharmacy.stock.read'
    WHEN 'pharmacy.operating_stock.batch_recorded' THEN permission='pharmacy.stock.receive'AND resource_kind='batch'AND resource_id IS NOT NULL
    ELSE permission='pharmacy.stock.receive'AND resource_kind='receipt'AND resource_id IS NOT NULL END
  END) NOT VALID;
ALTER TABLE pharmacy_operating_stock_audit
 VALIDATE CONSTRAINT pharmacy_operating_stock_audit_action_check,
 VALIDATE CONSTRAINT pharmacy_operating_stock_audit_resource_kind_check,
 VALIDATE CONSTRAINT pharmacy_operating_stock_audit_permission_check,
 VALIDATE CONSTRAINT pharmacy_operating_stock_audit_check;
