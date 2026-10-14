SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE UNIQUE INDEX workforce_staff_code_casefold ON workforce_staff (tenant_id, hospital_id, lower(employee_code));
