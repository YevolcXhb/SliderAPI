-- Add mcp_xml_inject field to groups table (for antigravity platform)
ALTER TABLE groups ADD COLUMN mcp_xml_inject TINYINT(1) NOT NULL DEFAULT true;
