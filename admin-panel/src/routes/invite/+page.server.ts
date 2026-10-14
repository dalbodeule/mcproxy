import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { acceptInvitation } from '#lib/server/membership.js';
import { createSession, SESSION_COOKIE } from '#lib/server/session.js';
import { panelMessage } from '#lib/server/messages.js';

export const load: PageServerLoad = ({ url }) => ({ token: url.searchParams.get('token') ?? '' });

export const actions: Actions = {
	default: async ({ request, cookies, url }) => {
		const form = await request.formData();
		const token = String(form.get('token') ?? '').trim();
		const password = String(form.get('password') ?? '');
		const confirmation = String(form.get('confirmation') ?? '');
		if (!token || password.length < 12 || password !== confirmation) return fail(400, { error: panelMessage(cookies, 'msg.inviteInvalid'), token });
		try {
			const member = acceptInvitation(token, password);
			if (!member) return fail(400, { error: panelMessage(cookies, 'msg.inviteExpired'), token });
		cookies.set(SESSION_COOKIE, createSession(member.username, { userId: member.id, role: member.role, mustChange: member.mustChangePassword }), { path: '/', httpOnly: true, sameSite: 'strict', secure: url.protocol === 'https:', maxAge: 8 * 60 * 60 });
		} catch {
			return fail(400, { error: panelMessage(cookies, 'msg.inviteFailed'), token });
		}
		throw redirect(303, '/');
	}
};
