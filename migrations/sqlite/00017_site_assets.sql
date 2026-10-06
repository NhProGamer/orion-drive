-- +goose Up
-- Admin-uploaded branding images (favicon, light/dark banners), keyed by
-- name. A table of their own: settings.value is TEXT, too small for images
-- on MySQL. etag is a content hash, so listing them never reads the data.
CREATE TABLE `site_assets` (
    `name` text PRIMARY KEY,
    `content_type` text,
    `etag` text,
    `data` blob,
    `updated_at` datetime
);

-- +goose Down
DROP TABLE `site_assets`;
