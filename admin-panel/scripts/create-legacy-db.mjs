import { DatabaseSync } from 'node:sqlite';
import { chmodSync } from 'node:fs';

const path = process.argv[2];
if (!path) throw new Error('usage: node create-legacy-db.mjs <db-path>');

const database = new DatabaseSync(path);
database.exec(`
  CREATE TABLE panel_users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'operator', 'auditor', 'viewer')),
    server_scope TEXT NOT NULL DEFAULT '[]',
    active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL
  );
`);
database.close();
chmodSync(path, 0o666);
console.log(`created legacy panel database at ${path}`);
