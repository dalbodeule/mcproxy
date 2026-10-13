import { createHmac, timingSafeEqual } from 'node:crypto';
import { panelSessionSecret } from './secrets.js';

const COOKIE = 'mcproxy_admin_session';

function signature(value: string) {
	return createHmac('sha256', panelSessionSecret()).update(value).digest('base64url');
}

export function isSessionConfigured() {
	return panelSessionSecret().length >= 32;
}

export function createSession(username: string) {
	const payload = Buffer.from(JSON.stringify({ username, exp: Date.now() + 8 * 60 * 60 * 1000 })).toString('base64url');
	return `${payload}.${signature(payload)}`;
}

export function verifySession(value: string | undefined) {
	if (!value || !isSessionConfigured()) return false;
	const parts = value.split('.');
	if (parts.length !== 2) return false;
	const [payload, provided] = parts;
	if (!payload || !provided) return false;
	const expected = signature(payload);
	const a = Buffer.from(provided);
	const b = Buffer.from(expected);
	if (a.length !== b.length || !timingSafeEqual(a, b)) return false;
	try {
		const decoded = JSON.parse(Buffer.from(payload, 'base64url').toString('utf8')) as { exp?: number };
		return typeof decoded.exp === 'number' && decoded.exp > Date.now();
	} catch {
		return false;
	}
}

export { COOKIE as SESSION_COOKIE };
