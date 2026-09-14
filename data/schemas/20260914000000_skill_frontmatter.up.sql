ALTER TABLE agent_skill ADD COLUMN IF NOT EXISTS version varchar(32) NOT NULL DEFAULT '';
ALTER TABLE agent_skill ADD COLUMN IF NOT EXISTS author varchar(128) NOT NULL DEFAULT '';
ALTER TABLE agent_skill ADD COLUMN IF NOT EXISTS license varchar(32) NOT NULL DEFAULT '';
ALTER TABLE agent_skill ADD COLUMN IF NOT EXISTS platform smallint NOT NULL DEFAULT 0;
ALTER TABLE agent_skill ADD COLUMN IF NOT EXISTS category varchar(32) NOT NULL DEFAULT '';
ALTER TABLE agent_skill ADD COLUMN IF NOT EXISTS homepage varchar(255) NOT NULL DEFAULT '';
ALTER TABLE agent_skill ADD COLUMN IF NOT EXISTS related_skills jsonb NOT NULL DEFAULT '[]';
ALTER TABLE agent_skill ALTER COLUMN description TYPE text;
