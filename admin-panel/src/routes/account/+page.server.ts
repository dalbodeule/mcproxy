import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { beginMFASetup, changeMemberPassword, confirmMFA, disableMFA, getMFAStatus, getMember } from '#lib/server/membership.js';
import { createSession, readSessionClaims, revokeAllSessions, SESSION_COOKIE } from '#lib/server/session.js';
import { panelMessage } from '#lib/server/messages.js';

function currentUser(cookies: Parameters<PageServerLoad>[0]['cookies']) {
	const claims = readSessionClaims(cookies.get(SESSION_COOKIE));
	if (!claims?.userId) throw redirect(303, '/login');
	const member = getMember(claims.userId);
	if (!member || member.username !== claims.username) throw redirect(303, '/login');
	return member;
}

export const load: PageServerLoad = ({ cookies }) => {
	const member = currentUser(cookies);
	return { username: member.username, mfaEnabled: getMFAStatus(member.id) };
};

export const actions: Actions = {
	changePassword: async ({ request, cookies, url }) => {
		const member = currentUser(cookies);
		const form = await request.formData();
		const currentPassword = String(form.get('current_password') ?? '');
		const nextPassword = String(form.get('new_password') ?? '');
		const confirmation = String(form.get('confirmation') ?? '');
		if (nextPassword.length < 12 || nextPassword !== confirmation) return fail(400, { error: panelMessage(cookies, 'msg.passwordMismatch') });
		if (!changeMemberPassword(member.id, currentPassword, nextPassword)) return fail(401, { error: panelMessage(cookies, 'msg.passwordCurrent') });
		revokeAllSessions(member.id);
		cookies.set(SESSION_COOKIE, createSession(member.username, { userId: member.id, role: member.role, mustChange: false }), { path: '/', httpOnly: true, sameSite: 'strict', secure: url.protocol === 'https:', maxAge: 8 * 60 * 60 });
		return { success: panelMessage(cookies, 'msg.passwordChanged') };
	},
	beginMFA: async ({ cookies }) => {
		const member = currentUser(cookies);
		if (getMFAStatus(member.id)) return fail(400, { error: panelMessage(cookies, 'msg.mfaAlready') });
		return { mfaSetup: beginMFASetup(member.id, member.username) };
	},
	confirmMFA: async ({ request, cookies, url }) => {
		const member = currentUser(cookies);
		const code = String((await request.formData()).get('code') ?? '');
		if (!confirmMFA(member.id, code, member.username)) return fail(400, { error: panelMessage(cookies, 'msg.mfaInvalid') });
		revokeAllSessions(member.id);
		cookies.set(SESSION_COOKIE, createSession(member.username, { userId: member.id, role: member.role, mustChange: member.mustChangePassword }), { path: '/', httpOnly: true, sameSite: 'strict', secure: url.protocol === 'https:', maxAge: 8 * 60 * 60 });
		return { success: panelMessage(cookies, 'msg.mfaEnabled') };
	},
	disableMFA: async ({ request, cookies, url }) => {
		const member = currentUser(cookies);
		const form = await request.formData();
		if (!disableMFA(member.id, String(form.get('password') ?? ''), String(form.get('code') ?? ''), member.username)) return fail(401, { error: panelMessage(cookies, 'msg.mfaDisableInvalid') });
		revokeAllSessions(member.id);
		cookies.set(SESSION_COOKIE, createSession(member.username, { userId: member.id, role: member.role, mustChange: member.mustChangePassword }), { path: '/', httpOnly: true, sameSite: 'strict', secure: url.protocol === 'https:', maxAge: 8 * 60 * 60 });
		return { success: panelMessage(cookies, 'msg.mfaDisabled') };
	}
};
