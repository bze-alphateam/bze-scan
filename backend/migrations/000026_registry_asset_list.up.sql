-- What the asset list of /api/v1/registry/assets needs from the chain
-- registry cache. logos: the logo_URIs and the first images entry, SVG and
-- PNG kept apart ({"logo_uris": {"svg", "png"}, "image": {"svg", "png"}});
-- logo_url stays the explorer's PNG-first pick. rest_urls / rpc_urls: the
-- chain.json apis addresses in the registry's order. description: the
-- assetlist.json asset's. No backfill: the next registry run fills them.
ALTER TABLE explorer.chains ADD COLUMN logos JSONB;
ALTER TABLE explorer.chains ADD COLUMN rest_urls TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE explorer.chains ADD COLUMN rpc_urls TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE explorer.registry_assets ADD COLUMN logos JSONB;
ALTER TABLE explorer.registry_assets ADD COLUMN description TEXT;
