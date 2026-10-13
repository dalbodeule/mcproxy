import { timingSafeEqual } from 'node:crypto';
import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { createSession, isSessionConfigured, SESSION_COOKIE, verifySession } from '#lib/server/session.js';
import { panelPassword, panelSessionSecret, panelUsername } from '#lib/server/secrets.js';

type LoginFailure = { started: number; count: number };
const loginFailures = new Map<string, LoginFailure>();
const LOGIN_WINDOW_MS = 60_000;
const LOGIN_FAILURE_LIMIT = 10;

function secretMatches(provided: string, expected: string) {
	const a = Buffer.from(provided);
	const b = Buffer.from(expected);
	return a.length === b.length && timingSafeEqual(a, b);
}

function allowLogin(ip: string, now = Date.now()) {
	const current = loginFailures.get(ip);
	const item = !current || now - current.started >= LOGIN_WINDOW_MS ? { started: now, count: 0 } : current;
	item.count += 1;
	loginFailures.set(ip, item);
	if (loginFailures.size > 10_000) loginFailures.delete(loginFailures.keys().next().value as string);
	return item.count <= LOGIN_FAILURE_LIMIT;
}

export const load: PageServerLoad = async ({ cookies }) => {
	if (verifySession(cookies.get(SESSION_COOKIE))) throw redirect(303, '/');
	return { configured: Boolean(panelPassword()) && panelSessionSecret().length >= 32 };
};

export const actions: Actions = {
	default: async ({ request, cookies, url, getClientAddress }) => {
		const form = await request.formData();
		const username = String(form.get('username') ?? '');
		const password = String(form.get('password') ?? '');
		if (!isSessionConfigured() || !panelPassword()) return fail(503, { error: 'Admin panel authentication is not configured.' });
		const clientIP = getClientAddress();
		if (!allowLogin(clientIP)) return fail(429, { error: 'Too many login attempts. Try again in one minute.' });
		if (!secretMatches(username, panelUsername()) || !secretMatches(password, panelPassword())) {
			return fail(401, { error: 'Invalid credentials.' });
		}
		loginFailures.delete(clientIP);
		cookies.set(SESSION_COOKIE, createSession(username), {
			path: '/',
			httpOnly: true,
			sameSite: 'strict',
			secure: url.protocol === 'https:',
			maxAge: 8 * 60 * 60
		});
		throw redirect(303, '/');
	}
};
