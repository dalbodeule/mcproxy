import { createServer } from 'node:http';

const base = 'http://127.0.0.1:3000';
const common = { 'X-Forwarded-Proto': 'http' };
let lastAuditURL = '';

const upstream = createServer((request, response) => {
	if (request.url?.startsWith('/api/v1/audits')) {
		lastAuditURL = request.url;
		response.setHeader('content-type', 'application/json');
		response.end(JSON.stringify({ items: [
			{ id: 1, server_id: 1, event: 'server.one' },
			{ id: 2, server_id: 2, event: 'server.two' }
		], limit: 20, offset: 0 }));
		return;
	}
	if (request.url?.startsWith('/api/v1/servers')) {
		response.setHeader('content-type', 'application/json');
		response.end(JSON.stringify({ items: [{ id: 1, name: 'one' }, { id: 2, name: 'two' }] }));
		return;
	}
	response.setHeader('content-type', 'application/json');
	response.end(JSON.stringify({ items: [] }));
});
await new Promise((resolve) => upstream.listen(18080, '127.0.0.1', () => resolve(undefined)));

/** @param {Response} response */
function cookieFrom(response) {
	const values = response.headers.getSetCookie?.() ?? [];
	const value = values[0] ?? response.headers.get('set-cookie') ?? '';
	return value.split(';')[0];
}

/**
 * @param {string} path
 * @param {Record<string, string>} values
 * @param {string} cookie
 * @param {RequestRedirect} redirect
 */
async function form(path, values, cookie, redirect = 'manual') {
	return fetch(`${base}${path}`, {
		method: 'POST',
		redirect,
		headers: { ...common, Origin: base, Cookie: cookie, 'Content-Type': 'application/x-www-form-urlencoded' },
		body: new URLSearchParams(values)
	});
}

let response = await form('/login', { username: 'admin', password: 'smoke-password-1234' }, '');
if (![200, 303].includes(response.status)) throw new Error(`admin login status ${response.status}`);
const adminCookie = cookieFrom(response);
if (!adminCookie) throw new Error('admin session cookie missing');

response = await form('/members?/createInvitation', { username: 'smoke-member', role: 'viewer', server_scope: '1' }, adminCookie, 'follow');
const invitationPage = await response.text();
if (!response.ok || !invitationPage.includes('/invite?token=')) throw new Error(`invitation creation failed: ${response.status}`);
// The action result contains both rendered HTML and SvelteKit's escaped
// serialized data. Restrict the capture to the base64url token alphabet so
// escaped quotes/backslashes cannot become part of the token.
const tokenMatch = invitationPage.match(/\/invite\?token=([A-Za-z0-9_-]+)/);
if (!tokenMatch) throw new Error('invitation token missing');

response = await form('/invite', { token: decodeURIComponent(tokenMatch[1]), password: 'smoke-member-password-1234', confirmation: 'smoke-member-password-1234' }, '', 'manual');
const memberCookie = cookieFrom(response);
if (![200, 303].includes(response.status) || !memberCookie) throw new Error(`invitation acceptance failed: ${response.status} ${await response.text()}`);

response = await fetch(`${base}/members`, { headers: { ...common, Cookie: adminCookie } });
const membersPage = await response.text();
const ids = [...membersPage.matchAll(/name="user_id" value="([^"]+)"/g)].map((match) => match[1]);
const memberID = ids.at(-1);
if (!memberID || !membersPage.includes('smoke-member')) throw new Error('accepted member not visible');

response = await form('/members?/updateMember', { user_id: memberID, role: 'operator', server_scope: '1' }, adminCookie, 'follow');
if (!response.ok) throw new Error(`member update failed: ${response.status}`);

response = await form('/members?/createInvitation', { username: 'smoke-scoped-admin', role: 'admin', server_scope: '1' }, adminCookie, 'follow');
const scopedInvitationPage = await response.text();
const scopedTokenMatch = scopedInvitationPage.match(/\/invite\?token=([A-Za-z0-9_-]+)/);
if (!response.ok || !scopedTokenMatch) throw new Error(`scoped admin invitation failed: ${response.status}`);
response = await form('/invite', { token: decodeURIComponent(scopedTokenMatch[1]), password: 'smoke-scoped-admin-password', confirmation: 'smoke-scoped-admin-password' }, '', 'manual');
const scopedAdminCookie = cookieFrom(response);
if (![200, 303].includes(response.status) || !scopedAdminCookie) throw new Error(`scoped admin acceptance failed: ${response.status} ${await response.text()}`);
response = await form('/members?/resetMFA', { user_id: memberID }, adminCookie, 'follow');
if (!response.ok) throw new Error(`member MFA reset failed: ${response.status}`);
response = await fetch(`${base}/`, { headers: { ...common, Cookie: memberCookie }, redirect: 'manual' });
if (response.status !== 303 || !(response.headers.get('location') ?? '').startsWith('/login')) throw new Error(`MFA reset did not revoke member session: ${response.status}`);
response = await form('/members?/setActive', { user_id: memberID, active: 'false' }, adminCookie, 'follow');
if (!response.ok) throw new Error(`member deactivation failed: ${response.status}`);
response = await form('/members?/createInvitation', { username: 'should-be-denied', role: 'viewer', server_scope: '' }, scopedAdminCookie, 'manual');
if (response.status !== 400) throw new Error(`scoped admin received unrestricted invitation: ${response.status}`);
response = await form('/members?/updateMember', { user_id: memberID, role: 'viewer', server_scope: '' }, scopedAdminCookie, 'manual');
if (response.status !== 400) throw new Error(`scoped admin widened an existing member: ${response.status}`);
response = await fetch(`${base}/api/v1/audits?server_id=1&server_id=2`, { headers: { ...common, Cookie: scopedAdminCookie } });
if (response.status !== 403) throw new Error(`scoped admin bypassed repeated server_id scope check: ${response.status}`);
response = await fetch(`${base}/api/v1/audits`, { headers: { ...common, Cookie: scopedAdminCookie } });
if (!response.ok) throw new Error(`scoped audit listing failed: ${response.status}`);
const scopedAudits = await response.json();
if (scopedAudits.items?.length !== 1 || scopedAudits.items[0].server_id !== 1) throw new Error(`scoped audit listing leaked another server: ${JSON.stringify(scopedAudits)}`);
const upstreamAuditIDs = new URL(lastAuditURL, 'http://127.0.0.1').searchParams.getAll('server_id');
if (upstreamAuditIDs.length !== 1 || upstreamAuditIDs[0] !== '1') throw new Error(`scoped audit query was not narrowed upstream: ${lastAuditURL}`);

response = await form('/members?/createInvitation', { username: 'smoke-member', role: 'viewer', server_scope: '1' }, adminCookie, 'manual');
if (response.status !== 400) throw new Error(`duplicate member invitation was accepted: ${response.status}`);
response = await form('/members?/createInvitation', { username: 'x'.repeat(129), role: 'viewer', server_scope: '1' }, adminCookie, 'manual');
if (response.status !== 400) throw new Error(`oversized member username was accepted: ${response.status}`);
response = await form('/members?/createInvitation', { username: 'smoke-auditor', role: 'auditor', server_scope: '1' }, adminCookie, 'follow');
const auditorInvitationPage = await response.text();
const auditorTokenMatch = auditorInvitationPage.match(/\/invite\?token=([A-Za-z0-9_-]+)/);
if (!response.ok || !auditorTokenMatch) throw new Error(`auditor invitation failed: ${response.status}`);
response = await form('/invite', { token: decodeURIComponent(auditorTokenMatch[1]), password: 'smoke-auditor-password', confirmation: 'smoke-auditor-password' }, '', 'manual');
const auditorCookie = cookieFrom(response);
if (![200, 303].includes(response.status) || !auditorCookie) throw new Error(`auditor acceptance failed: ${response.status} ${await response.text()}`);
response = await fetch(`${base}/members`, { headers: { ...common, Cookie: auditorCookie }, redirect: 'manual' });
if (response.status !== 303 || response.headers.get('location') !== '/') throw new Error(`auditor accessed member management: ${response.status} ${response.headers.get('location')}`);
console.log('Admin panel membership smoke test OK');
upstream.close();
