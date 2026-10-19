ALTER TABLE volumes
  ADD COLUMN IF NOT EXISTS reserved_to             character varying(50),
  ADD COLUMN IF NOT EXISTS reserved_at             timestamp without time zone,
  ADD COLUMN IF NOT EXISTS latest_reference             character varying(100),
  ADD COLUMN IF NOT EXISTS reservation_expires_at  timestamp without time zone;

CREATE INDEX IF NOT EXISTS idx_volumes_reservation_expires_at
  ON volumes (reservation_expires_at)
  WHERE reservation_expires_at IS NOT NULL;

  ALTER TABLE volumes
  ADD COLUMN IF NOT EXISTS sha256 character(64);