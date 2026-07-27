-- +goose Up
-- The default group's permissions carried a misleading `is_admin` key that never
-- matched the GroupPermissions schema (which uses `admin`), so it granted nothing
-- — administrator access is resolved via System.AdminEmails / System.AdminGroups.
-- Clear it so the stored value reflects reality: the Default group is not admin.
UPDATE `groups` SET `permissions` = '{}' WHERE `id` = 1;

-- +goose Down
UPDATE `groups` SET `permissions` = '{"is_admin":true}' WHERE `id` = 1;
