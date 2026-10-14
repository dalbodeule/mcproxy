import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { createSession, isSessionConfigured, SESSION_COOKIE, verifySession } from '#lib/server/session.js';
import { panelSessionSecret } from '#lib/server/secrets.js';
import { authenticateMember, verifyMemberMFA } from '#lib/server/membership.js';
import { panelMessage } from '#lib/server/messages.js';

type LoginFailure = { started: number; count: number };
const loginFailures = new Map<string, LoginFailure>();
const LOGIN_WINDOW_MS = 60_000;
const LOGIN_FAILURE_LIMIT = 10;

function safeReturnTo(value: string) {
	return value.startsWith('/') && !value.startsWith('//') ? value : '/';
}

function allowLogin(ip: string, now = Date.now()) {
	const current = loginFailures.get(ip);
	const item = !current || now - current.started >= LOGIN_WINDOW_MS ? { started: now, count: 0 } : current;
	item.count += 1;
	loginFailures.set(ip, item);
	if (loginFailures.size > 10_000) loginFailures.delete(loginFailures.keys().next().value as string);
	return item.count <= LOGIN_FAILURE_LIMIT;
}

export const load: PageServerLoad = async ({ cookies, url }) => {
	if (verifySession(cookies.get(SESSION_COOKIE))) throw redirect(303, '/');
	return { configured: panelSessionSecret().length >= 32, returnTo: safeReturnTo(url.searchParams.get('returnTo') ?? '/') };
};

export const actions: Actions = {
	default: async ({ request, cookies, url, getClientAddress }) => {
		const form = await request.formData();
		const returnTo = safeReturnTo(String(form.get('returnTo') ?? '/'));
		const username = String(form.get('username') ?? '');
		const password = String(form.get('password') ?? '');
		const otp = String(form.get('otp') ?? '');
		if (!isSessionConfigured()) return fail(503, { error: panelMessage(cookies, 'msg.authConfig') });
		const clientIP = getClientAddress();
		const usernameKey = `user:${username.trim().toLowerCase()}`;
		if (!allowLogin(`ip:${clientIP}`) || !allowLogin(usernameKey)) return fail(429, { error: panelMessage(cookies, 'msg.tooMany') });
		const member = authenticateMember(username, password);
		if (!member) {
			return fail(401, { error: panelMessage(cookies, 'msg.invalidCredentials') });
		}
		if (!verifyMemberMFA(member.id, otp)) return fail(401, { error: panelMessage(cookies, 'msg.mfaRequired'), mfaRequired: true, username });
		loginFailures.delete(`ip:${clientIP}`);
		loginFailures.delete(usernameKey);
		cookies.set(SESSION_COOKIE, createSession(member.username, { userId: member.id, role: member.role, mustChange: member.mustChangePassword }), {
			path: '/',
			httpOnly: true,
			sameSite: 'strict',
			secure: url.protocol === 'https:',
			maxAge: 8 * 60 * 60
		});
		throw redirect(303, returnTo);
	}
};
