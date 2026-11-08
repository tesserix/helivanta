-- ADR-0054 slice 3b-i: a pharmacy Staff record takes a reviewed account link; every other link-referencing table keeps its guard.
DROP TRIGGER workforce_hospital_establishment ON workforce_login_links;
