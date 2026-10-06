-- +goose Up
-- Admin-uploaded branding images (favicon, light/dark banners), keyed by
-- name. A table of their own: settings.value is TEXT, too small for images
-- on MySQL. etag is a content hash, so listing them never reads the data.
CREATE TABLE `site_assets` (
    `name` varchar(64) PRIMARY KEY,
    `content_type` varchar(100),
    `etag` varchar(64),
    `data` mediumblob,
    `updated_at` datetime
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE `site_assets`;
