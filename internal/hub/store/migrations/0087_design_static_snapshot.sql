-- compat: previous
-- Immutable raster pages are written atomically with their expiring share.
CREATE TABLE design_static_pages (
    link_id TEXT NOT NULL REFERENCES design_share_links(id) ON DELETE CASCADE,
    rev TEXT NOT NULL,
    position BIGINT NOT NULL,
    board TEXT NOT NULL,
    title TEXT NOT NULL,
    jpeg TEXT NOT NULL,
    PRIMARY KEY (link_id, position)
);
