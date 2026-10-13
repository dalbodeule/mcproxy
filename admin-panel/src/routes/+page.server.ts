import { fail } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import type { Audit, GeoPolicy, Policy, Rule, Server, Stats } from '#lib/types.js';
import { MCPROXY_API_URL } from '$app/env/private';
import { mcproxyAdminToken } from '#lib/server/secrets.js';

async function api(path: string, init?: RequestInit): Promise<Response> {
	const adminToken = mcproxyAdminToken();
	if (!MCPROXY_API_URL || !adminToken) {
		throw new Error('MCPROXY_API_URL and MCPROXY_ADMIN_TOKEN are required');
	}
	const headers = new Headers(init?.headers);
	headers.set('Authorization', `Bearer ${adminToken}`);
	return fetch(`${MCPROXY_API_URL}${path}`, {
		...init,
		headers
	});
}

export const load: PageServerLoad = async ({ url }) => {
	const auditEvent = url.searchParams.get('event') ?? '';
	const auditOffset = Math.max(0, Number(url.searchParams.get('offset') ?? 0) || 0);
	const auditLimit = 20;
	const [statsResponse, serversResponse, auditsResponse] = await Promise.all([
		api('/api/v1/stats'),
		api('/api/v1/servers/'),
		api(`/api/v1/audits?limit=${auditLimit}&offset=${auditOffset}${auditEvent ? `&event=${encodeURIComponent(auditEvent)}` : ''}`)
	]);
	const [policyResponse, geoResponse, rulesResponse] = await Promise.all([
		api('/api/v1/policy/global'),
		api('/api/v1/policy/geo'),
		api('/api/v1/rules')
	]);
	const stats = statsResponse.ok ? ((await statsResponse.json()) as Stats) : null;
	const servers = serversResponse.ok ? (((await serversResponse.json()) as { items: Server[] }).items ?? []) : [];
	const audits = auditsResponse.ok ? (((await auditsResponse.json()) as { items: Audit[] }).items ?? []) : [];
	const policy = policyResponse.ok ? ((await policyResponse.json()) as Policy) : null;
	const geo = geoResponse.ok ? ((await geoResponse.json()) as GeoPolicy) : null;
	const rules = rulesResponse.ok ? (((await rulesResponse.json()) as { items: Rule[] }).items ?? []) : [];
	const serverControls = Object.fromEntries(await Promise.all(servers.map(async (server) => {
		const [policyResult, geoResult, rulesResult] = await Promise.all([
			api(`/api/v1/policy/servers/${server.id}`),
			api(`/api/v1/policy/geo?server_id=${server.id}`),
			api(`/api/v1/rules?server_id=${server.id}`)
		]);
		let serverPolicy: Policy | null = null;
		if (policyResult.ok) {
			const value = (await policyResult.json()) as Policy & { policy?: Policy | null };
			serverPolicy = value.policy ?? (value.id ? value : null);
		}
		const serverGeo = geoResult.ok ? ((await geoResult.json()) as GeoPolicy) : null;
		const serverRules = rulesResult.ok ? (((await rulesResult.json()) as { items: Rule[] }).items ?? []) : [];
		return [server.id, { policy: serverPolicy, geo: serverGeo, rules: serverRules }];
	}))) as Record<number, { policy: Policy | null; geo: GeoPolicy | null; rules: Rule[] }>;
	const serverPolicies = Object.fromEntries(Object.entries(serverControls).map(([id, value]) => [id, value.policy])) as Record<number, Policy | null>;
	const serverGeos = Object.fromEntries(Object.entries(serverControls).map(([id, value]) => [id, value.geo])) as Record<number, GeoPolicy | null>;
	const serverRules = Object.fromEntries(Object.entries(serverControls).map(([id, value]) => [id, value.rules])) as Record<number, Rule[]>;
	return { stats, servers, audits, policy, geo, rules, serverPolicies, serverGeos, serverRules, auditEvent, auditOffset, auditLimit, auditHasNext: audits.length === auditLimit, apiAvailable: statsResponse.ok && serversResponse.ok && auditsResponse.ok };
};

export const actions: Actions = {
	updateServerPolicy: async ({ request }) => {
		const form = await request.formData();
		const serverID = String(form.get('server_id') ?? '');
		const number = (name: string) => Number(form.get(name) ?? 0);
		const payload = {
			ip_burst_10s: number('ip_burst_10s'),
			ip_burst_60s: number('ip_burst_60s'),
			name_burst_10s: number('name_burst_10s'),
			name_burst_60s: number('name_burst_60s'),
			deny_first: form.get('deny_first') === 'on'
		};
		if (!serverID || Object.values(payload).some((value) => typeof value === 'number' && (!Number.isInteger(value) || value < 0))) return fail(400, { error: 'Invalid server policy.' });
		const response = await api(`/api/v1/policy/servers/${encodeURIComponent(serverID)}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
		if (!response.ok) return fail(response.status, { error: await response.text() });
		return { success: `Server #${serverID} policy updated.` };
	},
	resetServerPolicy: async ({ request }) => {
		const form = await request.formData();
		const serverID = String(form.get('server_id') ?? '');
		if (!serverID) return fail(400, { error: 'Server is required.' });
		const response = await api(`/api/v1/policy/servers/${encodeURIComponent(serverID)}`, { method: 'DELETE' });
		if (!response.ok) return fail(response.status, { error: await response.text() });
		return { success: `Server #${serverID} now inherits the global policy.` };
	},
	updatePolicy: async ({ request }) => {
		const form = await request.formData();
		const number = (name: string) => Number(form.get(name) ?? 0);
		const payload = {
			ip_burst_10s: number('ip_burst_10s'),
			ip_burst_60s: number('ip_burst_60s'),
			name_burst_10s: number('name_burst_10s'),
			name_burst_60s: number('name_burst_60s'),
			deny_first: form.get('deny_first') === 'on',
			kick_message: String(form.get('kick_message') ?? '')
		};
		if (Object.values(payload).some((value) => typeof value === 'number' && (!Number.isInteger(value) || value < 0))) return fail(400, { error: 'Thresholds must be non-negative integers.' });
		const response = await api('/api/v1/policy/global', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
		if (!response.ok) return fail(response.status, { error: await response.text() });
		return { success: 'Global policy updated.' };
	},
	resetPolicy: async () => {
		const response = await api('/api/v1/policy/global', { method: 'DELETE' });
		if (!response.ok) return fail(response.status, { error: await response.text() });
		return { success: 'Global policy reset to defaults.' };
	},
	updateGeo: async ({ request }) => {
		const form = await request.formData();
		const serverID = String(form.get('server_id') ?? '');
		const countries = String(form.get('countries') ?? '').split(',').map((country) => country.trim().toUpperCase()).filter(Boolean);
		const query = serverID ? `?server_id=${encodeURIComponent(serverID)}` : '';
		const response = await api(`/api/v1/policy/geo${query}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ mode: String(form.get('mode') ?? 'disabled'), countries }) });
		if (!response.ok) return fail(response.status, { error: await response.text() });
		return { success: serverID ? `Server #${serverID} Geo policy updated.` : 'Geo policy updated.' };
	},
	resetGeo: async ({ request }) => {
		const form = await request.formData();
		const serverID = String(form.get('server_id') ?? '');
		const query = serverID ? `?server_id=${encodeURIComponent(serverID)}` : '';
		const response = await api(`/api/v1/policy/geo${query}`, { method: 'DELETE' });
		if (!response.ok) return fail(response.status, { error: await response.text() });
		return { success: serverID ? `Server #${serverID} Geo policy reset.` : 'Global Geo policy disabled.' };
	},
	createRule: async ({ request }) => {
		const form = await request.formData();
		const kind = String(form.get('kind') ?? '');
		const target = String(form.get('target') ?? '').trim();
		const serverID = String(form.get('server_id') ?? '');
		if (!target) return fail(400, { error: 'Rule target is required.' });
		const serverIDNumber: number | null = serverID ? Number(serverID) : null;
		if (serverIDNumber !== null && (!Number.isInteger(serverIDNumber) || serverIDNumber <= 0)) return fail(400, { error: 'Invalid server.' });
		const response = await api('/api/v1/rules/', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ kind, target, is_cidr: form.get('is_cidr') === 'on', reason: String(form.get('reason') ?? ''), ...(serverIDNumber !== null ? { server_id: serverIDNumber } : {}) }) });
		if (!response.ok) return fail(response.status, { error: await response.text() });
		return { success: serverID ? `Server #${serverID} rule created.` : 'Rule created.' };
	},
	removeRule: async ({ request }) => {
		const form = await request.formData();
		const id = String(form.get('id') ?? '');
		const response = await api(`/api/v1/rules/${encodeURIComponent(id)}/`, { method: 'DELETE' });
		if (!response.ok) return fail(response.status, { error: await response.text() });
		return { success: 'Rule deleted.' };
	},
	create: async ({ request }) => {
		const form = await request.formData();
		const name = String(form.get('name') ?? '').trim();
		const upstream = String(form.get('upstream') ?? '').trim();
		const forwardingMode = String(form.get('forwarding_mode') ?? '').trim();
		const publicHost = String(form.get('public_host') ?? '').trim();
		const publicAddr = String(form.get('public_addr') ?? '').trim();
		const pingMotd = String(form.get('ping_motd') ?? '').trim();
		const priority = Number(form.get('priority') ?? 0);
		if (!name || !upstream) return fail(400, { error: 'Name and upstream are required.' });
		if (!Number.isInteger(priority) || priority < 0 || priority > 1_000_000) return fail(400, { error: 'Priority must be between 0 and 1000000.' });
		const response = await api('/api/v1/servers/', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ name, upstream, forwarding_mode: forwardingMode, priority, ping_motd: pingMotd, ...(publicHost ? { public_host: publicHost } : {}), ...(publicAddr ? { public_addr: publicAddr } : {}) })
		});
		if (!response.ok) return fail(response.status, { error: await response.text() });
		return { success: 'Server created.' };
	},
	updateServer: async ({ request }) => {
		const form = await request.formData();
		const id = String(form.get('id') ?? '');
		const name = String(form.get('name') ?? '').trim();
		const upstream = String(form.get('upstream') ?? '').trim();
		const forwardingMode = String(form.get('forwarding_mode') ?? '').trim();
		const publicHost = String(form.get('public_host') ?? '').trim();
		const publicAddr = String(form.get('public_addr') ?? '').trim();
		const pingMotd = String(form.get('ping_motd') ?? '').trim();
		const priority = Number(form.get('priority') ?? 0);
		if (!id || !name || !upstream) return fail(400, { error: 'Server name and upstream are required.' });
		if (!Number.isInteger(priority) || priority < 0 || priority > 1_000_000) return fail(400, { error: 'Priority must be between 0 and 1000000.' });
		const response = await api(`/api/v1/servers/${encodeURIComponent(id)}/`, {
			method: 'PUT',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ name, upstream, forwarding_mode: forwardingMode, public_host: publicHost, public_addr: publicAddr, ping_motd: pingMotd, priority })
		});
		if (!response.ok) return fail(response.status, { error: await response.text() });
		return { success: `Server #${id} updated.` };
	},
	setEnabled: async ({ request }) => {
		const form = await request.formData();
		const id = String(form.get('id') ?? '');
		const enabled = form.get('enabled') === 'true';
		const response = await api(`/api/v1/servers/${encodeURIComponent(id)}/`, {
			method: 'PUT',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ enabled })
		});
		if (!response.ok) return fail(response.status, { error: await response.text() });
		return { success: enabled ? 'Server enabled.' : 'Server disabled.' };
	},
	rotateBungeeGuardToken: async ({ request }) => {
		const form = await request.formData();
		const id = String(form.get('id') ?? '');
		if (!id) return fail(400, { error: 'Server is required.' });
		const response = await api(`/api/v1/servers/${encodeURIComponent(id)}/bungeeguard-token`, { method: 'POST' });
		if (!response.ok) return fail(response.status, { error: await response.text() });
		return { success: `BungeeGuard token for server #${id} regenerated.` };
	},
	remove: async ({ request }) => {
		const form = await request.formData();
		const id = String(form.get('id') ?? '');
		const response = await api(`/api/v1/servers/${encodeURIComponent(id)}/`, { method: 'DELETE' });
		if (!response.ok) return fail(response.status, { error: await response.text() });
		return { success: 'Server deleted.' };
	}
};
