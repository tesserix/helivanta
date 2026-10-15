-- Leave reports (#1037 slice 4) scan request days and requests by date.
CREATE INDEX workforce_leave_request_days_day ON workforce_leave_request_days(tenant_id,hospital_id,day);
CREATE INDEX workforce_leave_requests_dates ON workforce_leave_requests(tenant_id,hospital_id,start_on,end_on);
