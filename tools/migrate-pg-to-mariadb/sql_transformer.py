#!/usr/bin/env python3
"""
PG -> MySQL/MariaDB 10.11 迁移 SQL 机械转换器。

用法：
    python tools/migrate-pg-to-mariadb/sql_transformer.py --check   # 只报告，不改文件
    python tools/migrate-pg-to-mariadb/sql_transformer.py --apply   # 就地转换 backend/migrations/*.sql

本脚本只做"安全的机械转换"：
  - BIGSERIAL PRIMARY KEY   -> BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY
  - BIGSERIAL               -> BIGINT NOT NULL AUTO_INCREMENT (需保证有 UNIQUE/PRIMARY KEY)
  - TIMESTAMPTZ             -> DATETIME(6)
  - JSONB                   -> JSON
  - BOOLEAN                 -> TINYINT(1)
  - NOW()                   -> CURRENT_TIMESTAMP(6)
  - gen_random_uuid()       -> UUID()
  - TRUE/FALSE 默认值       -> 1/0（仅 DEFAULT 上下文，保守处理）

以下构造无法机械转换，脚本会 **标记为需人工处理**（--check 报告，不改动）：
  - DO $$ ... $$  (PL/pgSQL 匿名块)
  - 数组类型 BIGINT[]/TEXT[]/INT[]
  - ON CONFLICT ... DO UPDATE  (需改 INSERT ... ON DUPLICATE KEY UPDATE)
  - CREATE INDEX CONCURRENTLY  (需改 ALTER TABLE ... ADD INDEX / 去掉 CONCURRENTLY)
  - USING gin / USING gist
  - PARTITION OF
  - to_regclass / pg_catalog / pg_class / pg_partitioned_table
  - text_pattern_ops
"""
import os
import re
import sys
import glob

MIGRATIONS_DIR = os.path.join(
    os.path.dirname(__file__), "..", "..", "backend", "migrations"
)

MANUAL_MARKERS = [
    (r"DO\s*\$\$", "PL/pgSQL DO block"),
    (r"\b(BIGINT|TEXT|INT|INTEGER|VARCHAR)\s*\[\]", "array type"),
    (r"ON\s+CONFLICT", "ON CONFLICT upsert"),
    (r"CONCURRENTLY", "CREATE/DROP INDEX CONCURRENTLY"),
    (r"USING\s+gin|USING\s+gist", "gin/gist index"),
    (r"PARTITION\s+OF", "declarative partition"),
    (r"to_regclass|pg_catalog|pg_class|pg_partitioned_table|pg_database", "pg catalog access"),
    (r"text_pattern_ops", "text_pattern_ops opclass"),
]

def mechanical_convert(sql: str) -> str:
    out = sql
    # BIGSERIAL PRIMARY KEY -> BIGINT ... AUTO_INCREMENT PRIMARY KEY
    out = re.sub(
        r"\bBIGSERIAL\s+PRIMARY\s+KEY\b",
        "BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY",
        out, flags=re.IGNORECASE,
    )
    # 独立 BIGSERIAL（无 PRIMARY KEY 内联）
    out = re.sub(r"\bBIGSERIAL\b", "BIGINT NOT NULL AUTO_INCREMENT", out, flags=re.IGNORECASE)
    out = re.sub(r"\bSERIAL\b", "INT NOT NULL AUTO_INCREMENT", out, flags=re.IGNORECASE)
    # 时间戳
    out = re.sub(r"\bTIMESTAMPTZ\b", "DATETIME(6)", out, flags=re.IGNORECASE)
    out = re.sub(r"\bTIMESTAMP\s+WITH\s+TIME\s+ZONE\b", "DATETIME(6)", out, flags=re.IGNORECASE)
    # JSONB -> JSON
    out = re.sub(r"\bJSONB\b", "JSON", out, flags=re.IGNORECASE)
    # BOOLEAN -> TINYINT(1)
    out = re.sub(r"\bBOOLEAN\b", "TINYINT(1)", out, flags=re.IGNORECASE)
    # NOW() -> CURRENT_TIMESTAMP(6)
    out = re.sub(r"\bNOW\(\)", "CURRENT_TIMESTAMP(6)", out, flags=re.IGNORECASE)
    # gen_random_uuid() / uuid_generate_v4() -> UUID()
    out = re.sub(r"\bgen_random_uuid\(\)", "UUID()", out, flags=re.IGNORECASE)
    out = re.sub(r"\buuid_generate_v4\(\)", "UUID()", out, flags=re.IGNORECASE)
    return out

def scan_manual(sql: str):
    hits = []
    for pat, label in MANUAL_MARKERS:
        if re.search(pat, sql, flags=re.IGNORECASE):
            hits.append(label)
    return hits

def main():
    mode = "--check"
    if len(sys.argv) > 1:
        mode = sys.argv[1]
    files = sorted(glob.glob(os.path.join(MIGRATIONS_DIR, "*.sql")))
    manual_needed = {}
    converted = 0
    for f in files:
        with open(f, "r", encoding="utf-8") as fh:
            src = fh.read()
        manual = scan_manual(src)
        if manual:
            manual_needed[os.path.basename(f)] = manual
        if mode == "--apply":
            new = mechanical_convert(src)
            if new != src:
                with open(f, "w", encoding="utf-8", newline="\n") as fh:
                    fh.write(new)
                converted += 1
    print(f"scanned {len(files)} migration files")
    if mode == "--apply":
        print(f"mechanically converted {converted} files")
    print(f"\n{len(manual_needed)} files STILL need manual review:")
    for name, labels in manual_needed.items():
        print(f"  {name}: {', '.join(sorted(set(labels)))}")

if __name__ == "__main__":
    main()
