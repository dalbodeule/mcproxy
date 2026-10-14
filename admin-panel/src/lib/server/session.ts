import { createHmac, randomBytes, timingSafeEqual } from 'node:crypto';
import { mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { ADMIN_PANEL_DB_PATH } from '$app/env/private';
import { panelSessionSecret } from './secrets.js';

const COOKIE = 'mcproxy_admin_session';

export type SessionClaims = { username: string; userId?: string; role?: string; mustChange?: boolean; sid?: string; exp: number };

let database: DatabaseSync | undefined;

function sessionDB() {
	if (!database) {
		const path = ADMIN_PANEL_DB_PATH || 'data/admin-panel.db';
		if (path !== ':memory:') mkdirSync(dirname(path), { recursive: true });
		database = new DatabaseSync(path);
		database.exec(`
			PRAGMA journal_mode = WAL;
			PRAGMA busy_timeout = 5000;
			CREATE TABLE IF NOT EXISTS panel_sessions (
				id TEXT PRIMARY KEY,
				user_id TEXT NOT NULL,
				expires_at INTEGER NOT NULL,
				revoked_at INTEGER
			);
			CREATE INDEX IF NOT EXISTS panel_sessions_user_idx ON panel_sessions(user_id);
			CREATE INDEX IF NOT EXISTS panel_sessions_expiry_idx ON panel_sessions(expires_at);
		`);
	}
	return database;
}

function signature(value: string) {
	return createHmac('sha256', panelSessionSecret()).update(value).digest('base64url');
}

export function isSessionConfigured() {
	return panelSessionSecret().length >= 32;
}

export function sessionStoreReady() {
	if (!isSessionConfigured()) return false;
	try {
		sessionDB().prepare('SELECT 1').get();
		return true;
	} catch {
		return false;
	}
}

export function createSession(username: string, claims: Partial<Pick<SessionClaims, 'userId' | 'role' | 'mustChange'>> = {}) {
	const exp = Date.now() + 8 * 60 * 60 * 1000;
	const userId = claims.userId;
	const sid = userId ? randomBytes(32).toString('base64url') : undefined;
	if (sid && userId) {
		const db = sessionDB();
		// Keep the session table bounded without deleting recently revoked rows
		// that may still be useful during incident investigation.
		db.prepare('DELETE FROM panel_sessions WHERE expires_at <= ? OR (revoked_at IS NOT NULL AND revoked_at <= ?)').run(Date.now(), Date.now() - 24 * 60 * 60 * 1000);
		db.prepare('INSERT INTO panel_sessions (id, user_id, expires_at) VALUES (?, ?, ?)').run(sid, userId, exp);
	}
	const payload = Buffer.from(JSON.stringify({ username, ...claims, ...(sid ? { sid } : {}), exp })).toString('base64url');
	return `${payload}.${signature(payload)}`;
}

export function verifySession(value: string | undefined) {
	return readSessionClaims(value) !== null;
}

export function readSession(value: string | undefined): string | null {
	return readSessionClaims(value)?.username ?? null;
}

export function readSessionClaims(value: string | undefined): SessionClaims | null {
	if (!value || !isSessionConfigured()) return null;
	const parts = value.split('.');
	if (parts.length !== 2) return null;
	const [payload, provided] = parts;
	if (!payload || !provided) return null;
	const expected = signature(payload);
	const a = Buffer.from(provided);
	const b = Buffer.from(expected);
	if (a.length !== b.length || !timingSafeEqual(a, b)) return null;
	try {
		const decoded = JSON.parse(Buffer.from(payload, 'base64url').toString('utf8')) as Partial<SessionClaims>;
		if (typeof decoded.username !== 'string' || decoded.username.length === 0 || typeof decoded.userId !== 'string' || typeof decoded.sid !== 'string' || typeof decoded.exp !== 'number' || decoded.exp <= Date.now()) return null;
		const stored = sessionDB().prepare('SELECT user_id, expires_at, revoked_at FROM panel_sessions WHERE id = ?').get(decoded.sid) as { user_id: string; expires_at: number; revoked_at: number | null } | undefined;
		if (!stored || stored.user_id !== decoded.userId || stored.revoked_at !== null || stored.expires_at <= Date.now()) return null;
		return { username: decoded.username, userId: decoded.userId, role: typeof decoded.role === 'string' ? decoded.role : undefined, mustChange: decoded.mustChange === true, sid: decoded.sid, exp: decoded.exp };
	} catch {
		return null;
	}
}

export function revokeSession(value: string | undefined) {
	const claims = readSessionClaims(value);
	if (claims?.sid) sessionDB().prepare('UPDATE panel_sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL').run(Date.now(), claims.sid);
}

export function revokeAllSessions(userId: string) {
	sessionDB().prepare('UPDATE panel_sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL').run(Date.now(), userId);
}

export { COOKIE as SESSION_COOKIE };
