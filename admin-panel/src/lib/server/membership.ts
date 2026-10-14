import { createCipheriv, createDecipheriv, createHash, createHmac, randomBytes, scryptSync, timingSafeEqual } from 'node:crypto';
import { mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { ADMIN_PANEL_DB_PATH } from '$app/env/private';
import { panelPassword, panelSessionSecret, panelUsername } from './secrets.js';
import { revokeAllSessions } from './session.js';

export const roles = ['owner', 'admin', 'operator', 'auditor', 'viewer'] as const;
export type Role = (typeof roles)[number];

const MIN_PASSWORD_LENGTH = 12;
const MAX_PASSWORD_LENGTH = 256;
const MAX_USERNAME_LENGTH = 128;
const MAX_SCOPE_ENTRIES = 256;
const MEMBERSHIP_SCHEMA_VERSION = 2;

export type Member = {
	id: string;
	username: string;
	role: Role;
	serverScope: number[];
	active: boolean;
	mustChangePassword: boolean;
	createdAt: string;
};

export type Invitation = {
	id: string;
	username: string;
	role: Role;
	serverScope: number[];
	expiresAt: string;
	usedAt: string | null;
	createdBy: string;
};

export type PanelAudit = {
	id: string;
	actor: string;
	event: string;
	subject: string;
	createdAt: string;
};

export type IncidentNote = {
	id: string;
	title: string;
	note: string;
	status: 'open' | 'closed';
	actor: string;
	createdAt: string;
	updatedAt: string;
};

let database: DatabaseSync | undefined;

function dbPath() {
	return ADMIN_PANEL_DB_PATH || 'data/admin-panel.db';
}

function db() {
	if (!database) {
		const path = dbPath();
		if (path !== ':memory:') mkdirSync(dirname(path), { recursive: true });
		database = new DatabaseSync(path);
		database.exec(`
			PRAGMA journal_mode = WAL;
			PRAGMA busy_timeout = 5000;
			CREATE TABLE IF NOT EXISTS panel_schema_meta (
				component TEXT PRIMARY KEY,
				version INTEGER NOT NULL
			);
			CREATE TABLE IF NOT EXISTS panel_users (
				id TEXT PRIMARY KEY,
				username TEXT NOT NULL UNIQUE COLLATE NOCASE,
				password_hash TEXT NOT NULL,
				role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'operator', 'auditor', 'viewer')),
				server_scope TEXT NOT NULL DEFAULT '[]',
				must_change_password INTEGER NOT NULL DEFAULT 0,
				active INTEGER NOT NULL DEFAULT 1,
				created_at TEXT NOT NULL
			);
			CREATE TABLE IF NOT EXISTS panel_invitations (
				id TEXT PRIMARY KEY,
				token_hash TEXT NOT NULL UNIQUE,
				username TEXT NOT NULL COLLATE NOCASE,
				role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'operator', 'auditor', 'viewer')),
				server_scope TEXT NOT NULL DEFAULT '[]',
				expires_at TEXT NOT NULL,
				used_at TEXT,
				created_by TEXT NOT NULL,
				created_at TEXT NOT NULL
			);
			CREATE TABLE IF NOT EXISTS panel_audit (
				id TEXT PRIMARY KEY,
				actor TEXT NOT NULL,
				event TEXT NOT NULL,
				subject TEXT NOT NULL,
				created_at TEXT NOT NULL
			);
			CREATE TABLE IF NOT EXISTS panel_incidents (
				id TEXT PRIMARY KEY,
				title TEXT NOT NULL,
				note TEXT NOT NULL,
				status TEXT NOT NULL CHECK (status IN ('open', 'closed')),
				created_by TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);
			CREATE TABLE IF NOT EXISTS panel_mfa (
				user_id TEXT PRIMARY KEY,
				secret_enc TEXT NOT NULL,
				enabled INTEGER NOT NULL DEFAULT 0,
				created_at TEXT NOT NULL,
				confirmed_at TEXT
			);
		`);
		migrateMembershipSchema(database);
	}
	return database;
}

export function membershipStoreReady() {
	try {
		const store = db();
		const row = store.prepare('SELECT version FROM panel_schema_meta WHERE component = ?').get('membership') as { version: number } | undefined;
		if (!row || Number(row.version) < MEMBERSHIP_SCHEMA_VERSION) return false;
		store.prepare('SELECT 1 FROM panel_users LIMIT 1').get();
		return true;
	} catch {
		return false;
	}
}

function migrateMembershipSchema(store: DatabaseSync) {
	const current = store.prepare('SELECT version FROM panel_schema_meta WHERE component = ?').get('membership') as { version: number } | undefined;
	if ((current?.version ?? 0) >= MEMBERSHIP_SCHEMA_VERSION) return;

	store.exec('BEGIN IMMEDIATE');
	try {
		// This migration is intentionally additive so existing SQLite databases
		// remain readable and can be upgraded in place during panel startup.
		try {
			store.exec('ALTER TABLE panel_users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0');
		} catch (error) {
			// A database created by the current schema already has the column. Any
			// other error (lock, corruption, permissions) must abort startup.
			if (!(error instanceof Error) || !/duplicate column name/i.test(error.message)) throw error;
		}
		store.prepare(`INSERT INTO panel_schema_meta (component, version) VALUES (?, ?) ON CONFLICT(component) DO UPDATE SET version = excluded.version`).run('membership', MEMBERSHIP_SCHEMA_VERSION);
		store.exec('COMMIT');
	} catch (error) {
		try { store.exec('ROLLBACK'); } catch { /* preserve the migration error */ }
		throw error;
	}
}

function recordAudit(store: DatabaseSync, actor: string, event: string, subject: string) {
	store.prepare('INSERT INTO panel_audit (id, actor, event, subject, created_at) VALUES (?, ?, ?, ?, ?)').run(id(), actor, event, subject, new Date().toISOString());
}

function id() {
	return randomBytes(16).toString('hex');
}

function normalizeRole(value: string): Role {
	return (roles as readonly string[]).includes(value) ? (value as Role) : 'viewer';
}

function normalizeScope(value: unknown): number[] {
	if (!Array.isArray(value)) return [];
	return [...new Set(value.map(Number).filter((item) => Number.isInteger(item) && item > 0))].slice(0, MAX_SCOPE_ENTRIES);
}

function hashPassword(password: string) {
	const salt = randomBytes(16);
	const derived = scryptSync(password, salt, 64, { N: 16_384, r: 8, p: 1, maxmem: 32 * 1024 * 1024 }).toString('base64url');
	return `scrypt$16384$8$1$${salt.toString('base64url')}$${derived}`;
}

function verifyPassword(password: string, encoded: string) {
	const [algorithm, n, r, p, salt, value] = encoded.split('$');
	if (algorithm !== 'scrypt' || !n || !r || !p || !salt || !value) return false;
	try {
		const expected = Buffer.from(value, 'base64url');
		const options = { N: Number(n), r: Number(r), p: Number(p), maxmem: 32 * 1024 * 1024 } as const;
		const actual = scryptSync(password, Buffer.from(salt, 'base64url'), expected.length, options);
		if (expected.length === actual.length && timingSafeEqual(expected, actual)) return true;
		// Compatibility with hashes created by the short-lived implementation that
		// passed the base64url salt text to scrypt instead of its decoded bytes.
		const legacy = scryptSync(password, salt, expected.length, options);
		return expected.length === legacy.length && timingSafeEqual(expected, legacy);
	} catch {
		return false;
	}
}

function ensureBootstrapUser() {
	const store = db();
	const count = Number((store.prepare('SELECT COUNT(*) AS count FROM panel_users').get() as { count: number }).count);
	const username = panelUsername().trim();
	const password = panelPassword();
	if (count === 0 && username && username.length <= MAX_USERNAME_LENGTH && password.length >= MIN_PASSWORD_LENGTH && password.length <= MAX_PASSWORD_LENGTH) {
		store.prepare('INSERT INTO panel_users (id, username, password_hash, role, created_at) VALUES (?, ?, ?, ?, ?)').run(id(), username, hashPassword(password), 'owner', new Date().toISOString());
	}
}

function mapMember(row: Record<string, unknown>): Member {
	return { id: String(row.id), username: String(row.username), role: normalizeRole(String(row.role)), serverScope: normalizeScope(JSON.parse(String(row.server_scope))), active: Number(row.active) === 1, mustChangePassword: Number(row.must_change_password) === 1, createdAt: String(row.created_at) };
}

export function authenticateMember(username: string, password: string): Member | null {
	ensureBootstrapUser();
	const normalizedUsername = username.trim();
	if (!normalizedUsername || normalizedUsername.length > MAX_USERNAME_LENGTH || password.length > MAX_PASSWORD_LENGTH) return null;
	const row = db().prepare('SELECT * FROM panel_users WHERE username = ? COLLATE NOCASE AND active = 1').get(normalizedUsername) as Record<string, unknown> | undefined;
	if (!row || !verifyPassword(password, String(row.password_hash))) return null;
	return mapMember(row);
}

export function changeMemberPassword(userId: string, currentPassword: string, nextPassword: string) {
	if (nextPassword.length < MIN_PASSWORD_LENGTH || nextPassword.length > MAX_PASSWORD_LENGTH || currentPassword.length > MAX_PASSWORD_LENGTH) return false;
	const store = db();
	const row = store.prepare('SELECT id, username, password_hash FROM panel_users WHERE id = ? AND active = 1').get(userId) as { id: string; username: string; password_hash: string } | undefined;
	if (!row || !verifyPassword(currentPassword, row.password_hash)) return false;
	store.prepare('UPDATE panel_users SET password_hash = ?, must_change_password = 0 WHERE id = ?').run(hashPassword(nextPassword), userId);
	recordAudit(store, row.username, 'member.password_change', row.username);
	return true;
}

function base32Encode(value: Buffer) {
	const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
	let bits = 0;
	let buffer = 0;
	let output = '';
	for (const byte of value) {
		buffer = (buffer << 8) | byte;
		bits += 8;
		while (bits >= 5) {
			output += alphabet[(buffer >>> (bits - 5)) & 31];
			bits -= 5;
		}
	}
	if (bits > 0) output += alphabet[(buffer << (5 - bits)) & 31];
	return output;
}

function base32Decode(value: string) {
	const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
	let bits = 0;
	let buffer = 0;
	const output: number[] = [];
	for (const char of value.toUpperCase().replace(/=+$/, '').replace(/[\s-]/g, '')) {
		const index = alphabet.indexOf(char);
		if (index < 0) throw new Error('invalid base32');
		buffer = (buffer << 5) | index;
		bits += 5;
		if (bits >= 8) {
			output.push((buffer >>> (bits - 8)) & 255);
			bits -= 8;
		}
	}
	return Buffer.from(output);
}

function mfaKey() {
	return createHash('sha256').update(panelSessionSecret()).digest();
}

function encryptMFASecret(secret: string) {
	const iv = randomBytes(12);
	const cipher = createCipheriv('aes-256-gcm', mfaKey(), iv);
	const ciphertext = Buffer.concat([cipher.update(secret, 'utf8'), cipher.final()]);
	return [iv, cipher.getAuthTag(), ciphertext].map((part) => part.toString('base64url')).join('.');
}

function decryptMFASecret(encoded: string) {
	const [ivEncoded, tagEncoded, ciphertextEncoded] = encoded.split('.');
	if (!ivEncoded || !tagEncoded || !ciphertextEncoded) throw new Error('invalid encrypted MFA secret');
	const decipher = createDecipheriv('aes-256-gcm', mfaKey(), Buffer.from(ivEncoded, 'base64url'));
	decipher.setAuthTag(Buffer.from(tagEncoded, 'base64url'));
	return Buffer.concat([decipher.update(Buffer.from(ciphertextEncoded, 'base64url')), decipher.final()]).toString('utf8');
}

function totp(secret: string, timestamp = Date.now()) {
	const counter = Math.floor(timestamp / 30_000);
	const message = Buffer.alloc(8);
	message.writeBigUInt64BE(BigInt(counter));
	const digest = createHmac('sha1', base32Decode(secret)).update(message).digest();
	const offset = digest[digest.length - 1] & 0x0f;
	const value = ((digest[offset] & 0x7f) << 24) | (digest[offset + 1] << 16) | (digest[offset + 2] << 8) | digest[offset + 3];
	return String(value % 1_000_000).padStart(6, '0');
}

function verifyTOTP(secret: string, code: string) {
	const normalized = code.replace(/\s/g, '');
	if (!/^\d{6}$/.test(normalized)) return false;
	for (const drift of [-1, 0, 1]) {
		const expected = totp(secret, Date.now() + drift * 30_000);
		if (timingSafeEqual(Buffer.from(expected), Buffer.from(normalized))) return true;
	}
	return false;
}

export function getMFAStatus(userId: string) {
	const row = db().prepare('SELECT enabled FROM panel_mfa WHERE user_id = ?').get(userId) as { enabled: number } | undefined;
	return Boolean(row && row.enabled === 1);
}

export function beginMFASetup(userId: string, username: string) {
	const secret = base32Encode(randomBytes(20));
	const store = db();
	store.prepare('INSERT INTO panel_mfa (user_id, secret_enc, enabled, created_at, confirmed_at) VALUES (?, ?, 0, ?, NULL) ON CONFLICT(user_id) DO UPDATE SET secret_enc = excluded.secret_enc, enabled = 0, created_at = excluded.created_at, confirmed_at = NULL').run(userId, encryptMFASecret(secret), new Date().toISOString());
	return { secret, uri: `otpauth://totp/mcproxy:${encodeURIComponent(username)}?secret=${secret}&issuer=mcproxy&algorithm=SHA1&digits=6&period=30` };
}

export function confirmMFA(userId: string, code: string, actor: string) {
	const row = db().prepare('SELECT secret_enc, enabled FROM panel_mfa WHERE user_id = ?').get(userId) as { secret_enc: string; enabled: number } | undefined;
	if (!row) return false;
	try {
		if (!verifyTOTP(decryptMFASecret(row.secret_enc), code)) return false;
	} catch {
		return false;
	}
	db().prepare('UPDATE panel_mfa SET enabled = 1, confirmed_at = ? WHERE user_id = ?').run(new Date().toISOString(), userId);
	recordAudit(db(), actor, 'member.mfa_enable', userId);
	return true;
}

export function verifyMemberMFA(userId: string, code: string) {
	const row = db().prepare('SELECT secret_enc, enabled FROM panel_mfa WHERE user_id = ?').get(userId) as { secret_enc: string; enabled: number } | undefined;
	if (!row || row.enabled !== 1) return true;
	try { return verifyTOTP(decryptMFASecret(row.secret_enc), code); } catch { return false; }
}

export function disableMFA(userId: string, password: string, code: string, actor: string) {
	const row = db().prepare('SELECT password_hash FROM panel_users WHERE id = ? AND active = 1').get(userId) as { password_hash: string } | undefined;
	if (!row || !verifyPassword(password, row.password_hash) || !getMFAStatus(userId) || !verifyMemberMFA(userId, code)) return false;
	db().prepare('DELETE FROM panel_mfa WHERE user_id = ?').run(userId);
	recordAudit(db(), actor, 'member.mfa_disable', userId);
	return true;
}

export function resetMemberMFA(userId: string, actor: string) {
	const store = db();
	const row = store.prepare('SELECT username FROM panel_users WHERE id = ? AND active = 1').get(userId) as { username: string } | undefined;
	if (!row) return false;
	store.prepare('DELETE FROM panel_mfa WHERE user_id = ?').run(userId);
	recordAudit(store, actor, 'member.mfa_reset', row.username);
	return true;
}

export function revokeMemberSessions(userId: string, actor: string) {
	const store = db();
	const row = store.prepare('SELECT username FROM panel_users WHERE id = ?').get(userId) as { username: string } | undefined;
	if (!row) return false;
	revokeAllSessions(userId);
	recordAudit(store, actor, 'member.sessions_revoke', row.username);
	return true;
}

export function resetMemberPassword(userId: string, actor: string) {
	const temporary = `${base32Encode(randomBytes(12)).slice(0, 8)}-${base32Encode(randomBytes(12)).slice(0, 8)}`;
	const store = db();
	const row = store.prepare('SELECT username FROM panel_users WHERE id = ? AND active = 1').get(userId) as { username: string } | undefined;
	if (!row) return null;
	store.prepare('UPDATE panel_users SET password_hash = ?, must_change_password = 1 WHERE id = ?').run(hashPassword(temporary), userId);
	recordAudit(store, actor, 'member.password_reset', row.username);
	return { username: row.username, temporary };
}

export function updateMember(userId: string, role: Role, serverScope: number[], actor: string) {
	if (!roles.includes(role)) return null;
	const store = db();
	store.exec('BEGIN IMMEDIATE');
	try {
		const row = store.prepare('SELECT username, role, active FROM panel_users WHERE id = ?').get(userId) as { username: string; role: string; active: number } | undefined;
		if (!row) {
			store.exec('COMMIT');
			return null;
		}
		if (row.active === 1 && row.role === 'owner' && role !== 'owner') {
			const owners = Number((store.prepare("SELECT COUNT(*) AS count FROM panel_users WHERE role = 'owner' AND active = 1").get() as { count: number }).count);
			if (owners <= 1) {
				store.exec('COMMIT');
				return false;
			}
		}
		store.prepare('UPDATE panel_users SET role = ?, server_scope = ? WHERE id = ?').run(role, JSON.stringify(normalizeScope(serverScope)), userId);
		recordAudit(store, actor, 'member.update', row.username);
		store.exec('COMMIT');
		return true;
	} catch (error) {
		store.exec('ROLLBACK');
		throw error;
	}
}

export function setMemberActive(userId: string, active: boolean, actor: string) {
	const store = db();
	store.exec('BEGIN IMMEDIATE');
	try {
		const row = store.prepare('SELECT username, role, active FROM panel_users WHERE id = ?').get(userId) as { username: string; role: string; active: number } | undefined;
		if (!row) {
			store.exec('COMMIT');
			return null;
		}
		if (!active && row.role === 'owner' && row.active === 1) {
			const owners = Number((store.prepare("SELECT COUNT(*) AS count FROM panel_users WHERE role = 'owner' AND active = 1").get() as { count: number }).count);
			if (owners <= 1) {
				store.exec('COMMIT');
				return false;
			}
		}
		store.prepare('UPDATE panel_users SET active = ? WHERE id = ?').run(active ? 1 : 0, userId);
		recordAudit(store, actor, active ? 'member.activate' : 'member.deactivate', row.username);
		store.exec('COMMIT');
		return true;
	} catch (error) {
		store.exec('ROLLBACK');
		throw error;
	}
}

export function getMember(idValue: string): Member | null {
	ensureBootstrapUser();
	const row = db().prepare('SELECT * FROM panel_users WHERE id = ? AND active = 1').get(idValue) as Record<string, unknown> | undefined;
	return row ? mapMember(row) : null;
}

export function listMembers(): Member[] {
	ensureBootstrapUser();
	return (db().prepare('SELECT * FROM panel_users ORDER BY username').all() as Record<string, unknown>[]).map(mapMember);
}

export function createInvitation(username: string, role: Role, serverScope: number[], createdBy: string, expiresInHours = 48) {
	ensureBootstrapUser();
	if (!username.trim() || username.trim().length > MAX_USERNAME_LENGTH || !roles.includes(role)) throw new Error('invalid invitation');
	const rawToken = randomBytes(32).toString('base64url');
	const now = new Date();
	const expiresAt = new Date(now.getTime() + Math.min(Math.max(expiresInHours, 1), 168) * 60 * 60 * 1000).toISOString();
	const store = db();
	const existing = store.prepare('SELECT 1 FROM panel_users WHERE username = ? COLLATE NOCASE').get(username.trim());
	if (existing) throw new Error('member already exists');
	const pending = store.prepare('SELECT 1 FROM panel_invitations WHERE username = ? COLLATE NOCASE AND used_at IS NULL AND expires_at > ?').get(username.trim(), now.toISOString());
	if (pending) throw new Error('invitation already exists');
	const invitationID = id();
	store.prepare('INSERT INTO panel_invitations (id, token_hash, username, role, server_scope, expires_at, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)').run(invitationID, createHash('sha256').update(rawToken).digest('hex'), username.trim(), role, JSON.stringify(normalizeScope(serverScope)), expiresAt, createdBy, now.toISOString());
	recordAudit(store, createdBy, 'member.invite', username.trim());
	return { id: invitationID, token: rawToken, expiresAt };
}

export function acceptInvitation(token: string, password: string) {
	ensureBootstrapUser();
	const store = db();
	const hash = createHash('sha256').update(token).digest('hex');
	store.exec('BEGIN IMMEDIATE');
	let username = '';
	try {
		const row = store.prepare('SELECT * FROM panel_invitations WHERE token_hash = ? AND used_at IS NULL').get(hash) as Record<string, unknown> | undefined;
		if (!row || Date.parse(String(row.expires_at)) <= Date.now() || password.length < MIN_PASSWORD_LENGTH || password.length > MAX_PASSWORD_LENGTH) {
			store.exec('COMMIT');
			return null;
		}
		const memberID = id();
		username = String(row.username);
		const existing = store.prepare('SELECT 1 FROM panel_users WHERE username = ? COLLATE NOCASE').get(username);
		if (existing) {
			store.exec('COMMIT');
			return null;
		}
		store.prepare('INSERT INTO panel_users (id, username, password_hash, role, server_scope, created_at) VALUES (?, ?, ?, ?, ?, ?)').run(memberID, username, hashPassword(password), normalizeRole(String(row.role)), String(row.server_scope), new Date().toISOString());
		store.prepare('UPDATE panel_invitations SET used_at = ? WHERE id = ?').run(new Date().toISOString(), String(row.id));
		recordAudit(store, username, 'member.accept', username);
		store.exec('COMMIT');
	} catch (error) {
		store.exec('ROLLBACK');
		throw error;
	}
	const user = store.prepare('SELECT * FROM panel_users WHERE username = ? COLLATE NOCASE AND active = 1').get(username) as Record<string, unknown>;
	return mapMember(user);
}

export function listInvitations(): Invitation[] {
	ensureBootstrapUser();
	return (db().prepare('SELECT * FROM panel_invitations ORDER BY created_at DESC').all() as Record<string, unknown>[]).map((row) => ({ id: String(row.id), username: String(row.username), role: normalizeRole(String(row.role)), serverScope: normalizeScope(JSON.parse(String(row.server_scope))), expiresAt: String(row.expires_at), usedAt: row.used_at ? String(row.used_at) : null, createdBy: String(row.created_by) }));
}

export function revokeInvitation(invitationID: string, actor: string) {
	const store = db();
	const row = store.prepare('SELECT username, used_at FROM panel_invitations WHERE id = ?').get(invitationID) as { username: string; used_at: string | null } | undefined;
	if (!row || row.used_at) return false;
	store.prepare('DELETE FROM panel_invitations WHERE id = ? AND used_at IS NULL').run(invitationID);
	recordAudit(store, actor, 'member.invite_revoke', row.username);
	return true;
}

export function listPanelAudits(): PanelAudit[] {
	ensureBootstrapUser();
	return (db().prepare('SELECT * FROM panel_audit ORDER BY created_at DESC LIMIT 100').all() as Record<string, unknown>[]).map((row) => ({ id: String(row.id), actor: String(row.actor), event: String(row.event), subject: String(row.subject), createdAt: String(row.created_at) }));
}

export function listIncidentNotes(): IncidentNote[] {
	ensureBootstrapUser();
	return (db().prepare('SELECT * FROM panel_incidents ORDER BY CASE status WHEN \'open\' THEN 0 ELSE 1 END, updated_at DESC LIMIT 100').all() as Record<string, unknown>[]).map((row) => ({
		id: String(row.id), title: String(row.title), note: String(row.note), status: row.status === 'closed' ? 'closed' : 'open', actor: String(row.created_by), createdAt: String(row.created_at), updatedAt: String(row.updated_at)
	}));
}

export function createIncidentNote(title: string, note: string, actor: string) {
	const normalizedTitle = title.trim();
	const normalizedNote = note.trim();
	if (!normalizedTitle || normalizedTitle.length > 200 || !normalizedNote || normalizedNote.length > 10_000) return null;
	const store = db();
	const now = new Date().toISOString();
	const incidentID = id();
	store.prepare('INSERT INTO panel_incidents (id, title, note, status, created_by, created_at, updated_at) VALUES (?, ?, ?, \'open\', ?, ?, ?)').run(incidentID, normalizedTitle, normalizedNote, actor, now, now);
	recordAudit(store, actor, 'incident.create', normalizedTitle);
	return incidentID;
}

export function closeIncidentNote(incidentID: string, actor: string) {
	const store = db();
	const row = store.prepare('SELECT title, status FROM panel_incidents WHERE id = ?').get(incidentID) as { title: string; status: string } | undefined;
	if (!row || row.status === 'closed') return false;
	store.prepare('UPDATE panel_incidents SET status = \'closed\', updated_at = ? WHERE id = ? AND status = \'open\'').run(new Date().toISOString(), incidentID);
	recordAudit(store, actor, 'incident.close', row.title);
	return true;
}
