import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { createInvitation, getMember, listInvitations, listMembers, listPanelAudits, resetMemberMFA, resetMemberPassword, roles, setMemberActive, updateMember, type Role } from '#lib/server/membership.js';
import { readSessionClaims, revokeAllSessions, SESSION_COOKIE } from '#lib/server/session.js';
import { panelMessage } from '#lib/server/messages.js';

const MAX_SCOPE_INPUT_LENGTH = 4096;
const MAX_SCOPE_ENTRIES = 256;

function parseScope(value: string) {
	if (value.length > MAX_SCOPE_INPUT_LENGTH) return null;
	const items = value.split(',').map((item) => Number(item.trim())).filter((item) => Number.isInteger(item) && item > 0);
	if (items.length > MAX_SCOPE_ENTRIES) return null;
	return [...new Set(items)];
}

function currentUser(cookies: Parameters<PageServerLoad>[0]['cookies']) {
	const claims = readSessionClaims(cookies.get(SESSION_COOKIE));
	if (!claims?.userId || !claims.role) throw redirect(303, '/login');
	const member = getMember(claims.userId);
	if (!member || member.username !== claims.username) throw redirect(303, '/login');
	if (member.role !== 'owner' && member.role !== 'admin') throw redirect(303, '/');
	return { ...claims, userId: member.id, username: member.username, role: member.role, serverScope: member.serverScope };
}

function canAssignScope(actor: { role: Role; serverScope: number[] }, requested: number[]) {
	if (actor.role === 'owner' || actor.serverScope.length === 0) return true;
	// An empty scope means all servers, so a scoped administrator must never
	// be able to create or promote an unrestricted member.
	return requested.length > 0 && requested.every((serverID) => actor.serverScope.includes(serverID));
}

function canManageMember(actor: { role: Role; serverScope: number[] }, target: { role: Role; serverScope: number[] }) {
	if (actor.role === 'owner' || actor.serverScope.length === 0) return true;
	return target.serverScope.length > 0 && target.serverScope.every((serverID) => actor.serverScope.includes(serverID));
}

export const load: PageServerLoad = ({ cookies }) => {
	const claims = currentUser(cookies);
	return { members: listMembers(), invitations: listInvitations(), audits: listPanelAudits(), role: claims.role, currentUserId: claims.userId };
};

export const actions: Actions = {
	createInvitation: async ({ request, cookies, url }) => {
		const claims = currentUser(cookies);
		if (claims.role !== 'owner' && claims.role !== 'admin') return fail(403, { error: panelMessage(cookies, 'msg.permission') });
		const form = await request.formData();
		const username = String(form.get('username') ?? '').trim();
		const role = String(form.get('role') ?? 'viewer') as Role;
		const scopeText = String(form.get('server_scope') ?? '');
		const scope = parseScope(scopeText);
		if (!username || !roles.includes(role) || scope === null || (role === 'owner' && claims.role !== 'owner') || !canAssignScope(claims, scope)) return fail(400, { error: panelMessage(cookies, 'msg.invalidMember') });
		try {
			const invitation = createInvitation(username, role, scope, claims.username);
			return { success: panelMessage(cookies, 'msg.invitationCreated'), inviteURL: `${url.origin}/invite?token=${encodeURIComponent(invitation.token)}`, expiresAt: invitation.expiresAt };
		} catch {
			return fail(400, { error: panelMessage(cookies, 'msg.invitationFailed') });
		}
	},
	resetPassword: async ({ request, cookies }) => {
		const claims = currentUser(cookies);
		if (claims.role !== 'owner' && claims.role !== 'admin') return fail(403, { error: panelMessage(cookies, 'msg.permission') });
		const userId = String((await request.formData()).get('user_id') ?? '');
		const target = listMembers().find((member) => member.id === userId);
		const actor = getMember(claims.userId);
		if (!target || !actor || target.id === claims.userId || (claims.role === 'admin' && target.role === 'owner') || !canManageMember(actor, target)) return fail(400, { error: panelMessage(cookies, 'msg.memberCannotChange') });
		const result = resetMemberPassword(target.id, claims.username);
		if (!result) return fail(404, { error: panelMessage(cookies, 'msg.memberNotFound') });
		revokeAllSessions(target.id);
		return { success: panelMessage(cookies, 'msg.passwordReset', { username: result.username }), temporaryPassword: result.temporary };
	},
	resetMFA: async ({ request, cookies }) => {
		const claims = currentUser(cookies);
		if (claims.role !== 'owner' && claims.role !== 'admin') return fail(403, { error: panelMessage(cookies, 'msg.permission') });
		const userId = String((await request.formData()).get('user_id') ?? '');
		const target = listMembers().find((member) => member.id === userId);
		const actor = getMember(claims.userId);
		if (!target || !actor || target.id === claims.userId || (claims.role === 'admin' && target.role === 'owner') || !canManageMember(actor, target)) return fail(400, { error: panelMessage(cookies, 'msg.memberCannotChange') });
		if (!resetMemberMFA(target.id, claims.username)) return fail(404, { error: panelMessage(cookies, 'msg.memberNotFound') });
		revokeAllSessions(target.id);
		return { success: panelMessage(cookies, 'msg.mfaReset', { username: target.username }) };
	},
	updateMember: async ({ request, cookies }) => {
		const claims = currentUser(cookies);
		if (claims.role !== 'owner' && claims.role !== 'admin') return fail(403, { error: panelMessage(cookies, 'msg.permission') });
		const form = await request.formData();
		const userId = String(form.get('user_id') ?? '');
		const target = listMembers().find((member) => member.id === userId);
		const role = String(form.get('role') ?? '') as Role;
		const scope = parseScope(String(form.get('server_scope') ?? ''));
		const actor = getMember(claims.userId);
		if (!target || !actor || scope === null || target.id === claims.userId || (claims.role === 'admin' && (target.role === 'owner' || role === 'owner')) || !roles.includes(role) || !canManageMember(actor, target) || !canAssignScope(actor, scope)) return fail(400, { error: panelMessage(cookies, 'msg.memberCannotChange') });
		if (!updateMember(target.id, role, scope, claims.username)) return fail(404, { error: panelMessage(cookies, 'msg.memberNotFound') });
		revokeAllSessions(target.id);
		return { success: panelMessage(cookies, 'msg.memberUpdated') };
	},
	setActive: async ({ request, cookies }) => {
		const claims = currentUser(cookies);
		if (claims.role !== 'owner' && claims.role !== 'admin') return fail(403, { error: panelMessage(cookies, 'msg.permission') });
		const form = await request.formData();
		const userId = String(form.get('user_id') ?? '');
		const active = String(form.get('active') ?? '') === 'true';
		const target = listMembers().find((member) => member.id === userId);
		const actor = getMember(claims.userId);
		if (!target || !actor || target.id === claims.userId || (claims.role === 'admin' && target.role === 'owner') || !canManageMember(actor, target)) return fail(400, { error: panelMessage(cookies, 'msg.memberCannotChange') });
		const result = setMemberActive(target.id, active, claims.username);
		if (result === false) return fail(400, { error: panelMessage(cookies, 'msg.lastOwner') });
		if (result === null) return fail(404, { error: panelMessage(cookies, 'msg.memberNotFound') });
		if (!active) revokeAllSessions(target.id);
		return { success: panelMessage(cookies, active ? 'msg.memberActivated' : 'msg.memberDeactivated') };
	}
};
