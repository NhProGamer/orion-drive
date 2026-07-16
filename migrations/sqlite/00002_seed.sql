-- +goose Up
-- Default local storage policy and default group. base_path is the on-disk
-- root used by the local driver (empty falls back to data/storage).
INSERT INTO `storage_policies` (`id`, `name`, `type`, `base_path`)
VALUES (1, 'Default local', 'local', 'data/storage');

INSERT INTO `groups` (`id`, `name`, `max_storage`, `storage_policy_id`, `permissions`)
VALUES (1, 'Default', 53687091200, 1, '{"is_admin":true}');

-- +goose Down
DELETE FROM `groups` WHERE `id` = 1;
DELETE FROM `storage_policies` WHERE `id` = 1;
