-- A private, unpublished initial clinic grants the first manager a scope.
-- Business address, phone and public publication must be completed by the owner.
INSERT INTO clinics (id, name, address, phone, active)
SELECT 'clinic-bailiankai', '百莲凯医疗美容', '', '', TRUE
WHERE NOT EXISTS (SELECT 1 FROM clinics);
