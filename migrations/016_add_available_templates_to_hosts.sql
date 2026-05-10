-- migration 016: Add available_templates to hosts table
ALTER TABLE hosts ADD COLUMN IF NOT EXISTS available_templates TEXT[] DEFAULT '{}';
