import { fail } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import type { Audit, GeoPolicy, Policy, Rule, RuntimeMetrics, Server, ServerHealth, Stats } from '#lib/types.js';
import { normalizeLocale, translate } from '#lib/i18n.js';
import { MCPROXY_API_URL } from '$app/env/private';
import { createHmac } from 'node:crypto';
import { mcproxyAdminActorSecret, mcproxyAdminToken } from '#lib/server/secrets.js';
import { readSessionClaims, SESSION_COOKIE } from '#lib/server/session.js';
import { closeIncidentNote, createIncidentNote, getMember, listIncidentNotes } from '#lib/server/membership.js';

type PanelCookies = Parameters<PageServerLoad>[0]['cookies'];

function message(cookies: PanelCookies, key: string, values: Record<string, string | number> = {}) {
	let text = translate(normalizeLocale(cookies.get('mcproxy_locale')), key);
	for (const [name, value] of Object.entries(values)) text = text.replace(`{${name}}`, String(value));
	return text;
}

function activeMember(cookies: PanelCookies) {
	const claims = readSessionClaims(cookies.get(SESSION_COOKIE));
	if (!claims?.userId) return null;
	const member = getMember(claims.userId);
	if (!member || member.username !== claims.username) return null;
	return member;
}

function panelActor(cookies: PanelCookies) {
	return activeMember(cookies)?.username ?? '';
}

function requireRole(cookies: PanelCookies, allowed: string[]) {
	const member = activeMember(cookies);
	if (!member) return fail(401, { error: message(cookies, 'msg.authRequired') });
	if (!allowed.includes(member.role)) return fail(403, { error: message(cookies, 'msg.permission') });
	return null;
}

function requireServerScope(cookies: PanelCookies, serverID: string) {
	const member = activeMember(cookies);
	const id = Number(serverID);
	if (!member || !Number.isInteger(id) || id <= 0) return fail(403, { error: message(cookies, 'msg.scope') });
	if (member.role !== 'owner' && member.serverScope.length > 0 && !member.serverScope.includes(id)) return fail(403, { error: message(cookies, 'msg.scope') });
	return null;
}

function requireUnrestrictedMember(cookies: PanelCookies) {
	const member = activeMember(cookies);
	if (!member) return fail(401, { error: message(cookies, 'msg.authRequired') });
	if (member.role !== 'owner' && member.serverScope.length > 0) return fail(403, { error: message(cookies, 'msg.unrestricted') });
	return null;
}

async function requireRuleScope(cookies: PanelCookies, ruleID: string, serverIDHint = '') {
	const member = activeMember(cookies);
	if (!member) return fail(401, { error: message(cookies, 'msg.authRequired') });
	const pageSize = 100;
	let offset = 0;
	let rule: { id?: unknown; server_id?: unknown } | undefined;
	while (!rule) {
		const serverQuery = serverIDHint ? `&server_id=${encodeURIComponent(serverIDHint)}` : '';
		const response = await api(`/api/v1/rules?limit=${pageSize}&offset=${offset}${serverQuery}`, undefined, member.username);
		if (!response.ok) return fail(502, { error: message(cookies, 'msg.invalidServer') });
		const payload = await response.json() as { items?: Array<{ id?: unknown; server_id?: unknown }>; has_more?: boolean };
		rule = payload.items?.find((item) => String(item.id) === ruleID);
		if (rule || payload.has_more !== true) break;
		offset += pageSize;
	}
	if (!rule) return fail(404, { error: message(cookies, 'msg.invalidServer') });
	const serverID = Number(rule.server_id);
	if (!Number.isInteger(serverID) || serverID <= 0) return requireUnrestrictedMember(cookies);
	if (member.role !== 'owner' && member.serverScope.length > 0 && !member.serverScope.includes(serverID)) return fail(403, { error: message(cookies, 'msg.scope') });
	return null;
}

async function api(path: string, init?: RequestInit, actor = ''): Promise<Response> {
	const adminToken = mcproxyAdminToken();
	if (!MCPROXY_API_URL || !adminToken) {
		return new Response('Admin panel upstream is not configured', { status: 503 });
	}
	const headers = new Headers(init?.headers);
	headers.set('Authorization', `Bearer ${adminToken}`);
	const actorSecret = mcproxyAdminActorSecret();
	if (actor && actorSecret) {
		headers.set('X-Admin-Actor', actor);
		headers.set('X-Admin-Actor-Signature', createHmac('sha256', actorSecret).update(actor).digest('hex'));
	}
	try {
		return await fetch(`${MCPROXY_API_URL}${path}`, {
			...init,
			headers,
			signal: init?.signal ?? AbortSignal.timeout(15_000)
		});
	} catch {
		return new Response('Admin panel upstream timed out or is unavailable', { status: 504 });
	}
}

async function jsonOrNull<T>(response: Response): Promise<T | null> {
	if (!response.ok || response.status === 204) return null;
	try {
		return await response.json() as T;
	} catch {
		return null;
	}
}

async function upstreamFailure(response: Response, cookies: PanelCookies) {
	const raw = (await response.text()).trim();
	let detail = raw;
	try {
		const payload = JSON.parse(raw) as { error?: unknown; message?: unknown };
		const value = typeof payload.error === 'string' ? payload.error : payload.message;
		if (typeof value === 'string') detail = value;
	} catch {
		// Preserve plain-text upstream diagnostics below.
	}
	detail = detail.replace(/\s+/g, ' ').slice(0, 512);
	const retryHint = message(cookies, 'msg.retryHint');
	return detail ? `${message(cookies, 'msg.upstreamFailure')}: ${detail} ${retryHint}` : `${message(cookies, 'msg.upstreamFailure')} ${retryHint}`;
}

function metricValue(body: string, name: string): number {
	const match = body.match(new RegExp(`^${name}\\s+(-?\\d+(?:\\.\\d+)?)$`, 'm'));
	return match ? Number(match[1]) : 0;
}

function metricLabelValue(body: string, name: string, label: string): string {
	const line = body.split('\n').find((value) => value.startsWith(`${name}{`));
	if (!line) return '';
	const match = line.match(new RegExp(`${label}="((?:\\\\.|[^"])*)"`));
	if (!match) return '';
	return match[1].replace(/\\n/g, '\n').replace(/\\"/g, '"').replace(/\\\\/g, '\\');
}

function distributionRouteKeys(server: Server): string[] {
	const values = [server.public_host ?? '', server.public_addr ?? ''];
	const keys = new Set<string>();
	for (const value of values) {
		const normalized = value.trim().toLowerCase();
		if (!normalized) continue;
		keys.add(normalized);
		if (value === server.public_addr) {
			const separator = normalized.lastIndexOf(':');
			if (separator > 0) keys.add(normalized.slice(0, separator));
		}
	}
	return [...keys];
}

export const load: PageServerLoad = async ({ url, cookies }) => {
	const requestedView = url.searchParams.get('view') ?? 'dashboard';
	const view = ['dashboard', 'policies', 'servers', 'audit'].includes(requestedView) ? requestedView : 'dashboard';
	const auditEvent = url.searchParams.get('event') ?? '';
	const auditActor = (url.searchParams.get('actor') ?? '').trim();
	const auditOffset = Math.max(0, Number(url.searchParams.get('offset') ?? 0) || 0);
	const auditLimit = 20;
	const ruleLimit = 25;
	const ruleOffset = Math.max(0, Number(url.searchParams.get('rule_offset') ?? 0) || 0);
	const member = activeMember(cookies);
	const incidentNotes = member ? listIncidentNotes() : [];
	const auditScope = member && member.role !== 'owner' && member.serverScope.length > 0
		? member.serverScope.map((id) => `server_id=${encodeURIComponent(id)}`).join('&')
		: '';
	const [statsResponse, serversResponse, auditsResponse, metricsResponse] = await Promise.all([
		api('/api/v1/stats', undefined, member?.username),
		api('/api/v1/servers/', undefined, member?.username),
		api(`/api/v1/audits?limit=${auditLimit}&offset=${auditOffset}${auditEvent ? `&event=${encodeURIComponent(auditEvent)}` : ''}${auditActor ? `&actor=${encodeURIComponent(auditActor)}` : ''}${auditScope ? `&${auditScope}` : ''}`, undefined, member?.username),
		api('/metrics', undefined, member?.username)
	]);
	const needsPolicyData = view === 'policies';
	const [policyResponse, geoResponse, rulesResponse] = needsPolicyData
		? await Promise.all([
			api('/api/v1/policy/global', undefined, member?.username),
			api('/api/v1/policy/geo', undefined, member?.username),
			api(`/api/v1/rules?limit=${ruleLimit}&offset=${ruleOffset}`, undefined, member?.username)
		])
		: [new Response(null, { status: 204 }), new Response(null, { status: 204 }), new Response(null, { status: 204 })];
	const stats = statsResponse.ok ? ((await statsResponse.json()) as Stats) : null;
	const allServers = serversResponse.ok ? (((await serversResponse.json()) as { items: Server[] }).items ?? []) : [];
	const scopedServers = member && member.role !== 'owner' && member.serverScope.length > 0 ? allServers.filter((server) => member.serverScope.includes(server.id)) : allServers;
	const serverSearch = (url.searchParams.get('server_q') ?? '').trim().toLowerCase();
	const serverEnabledFilter = ['enabled', 'disabled'].includes(url.searchParams.get('server_enabled') ?? '') ? url.searchParams.get('server_enabled')! : '';
	const filteredServers = scopedServers.filter((server) => (!serverSearch || [server.name, server.upstream, server.public_host ?? '', server.public_addr ?? ''].some((value) => value.toLowerCase().includes(serverSearch))) && (!serverEnabledFilter || (serverEnabledFilter === 'enabled' ? server.enabled : !server.enabled)));
	const serverLimit = 25;
	const requestedServerOffset = Math.max(0, Number(url.searchParams.get('server_offset') ?? 0) || 0);
	const serverOffset = filteredServers.length === 0 ? 0 : Math.min(requestedServerOffset, Math.floor((filteredServers.length - 1) / serverLimit) * serverLimit);
	const displayedServers = view === 'servers' ? filteredServers.slice(serverOffset, serverOffset + serverLimit) : filteredServers;
	const canViewBungeeGuardTokens = member?.role === 'owner' || member?.role === 'admin';
	const servers = canViewBungeeGuardTokens ? displayedServers : displayedServers.map((server) => ({ ...server, bungeeguard_allowed_token: undefined }));
	const enabledServers = filteredServers.filter((server) => server.enabled);
	const distributionMap = new Map<string, Array<{ id: number; name: string }>>();
	if (enabledServers.length > 1) {
		distributionMap.set('default', enabledServers.map((server) => ({ id: server.id, name: server.name })));
	}
	for (const server of enabledServers) {
		for (const route of distributionRouteKeys(server)) {
			const current = distributionMap.get(route) ?? [];
			if (!current.some((backend) => backend.id === server.id)) current.push({ id: server.id, name: server.name });
			distributionMap.set(route, current);
		}
	}
	const distributionGroups = [...distributionMap].filter(([, backends]) => backends.length > 1).map(([route, backends]) => ({ route, backends }));
	const auditsPayload = auditsResponse.ok ? await auditsResponse.json() as { items?: Audit[]; has_more?: boolean } : null;
	const audits = auditsPayload?.items ?? [];
	const auditHasNext = auditsPayload?.has_more === true;
	const metricsText = metricsResponse.ok ? await metricsResponse.text() : '';
	const metrics: RuntimeMetrics = {
		build_version: metricLabelValue(metricsText, 'mcproxy_build_info', 'version'),
		build_commit: metricLabelValue(metricsText, 'mcproxy_build_info', 'commit'),
		build_time: metricLabelValue(metricsText, 'mcproxy_build_info', 'build_time'),
		gate_ready: metricValue(metricsText, 'mcproxy_gate_ready'),
		gate_players: metricValue(metricsText, 'mcproxy_gate_players'),
		gate_backends: metricValue(metricsText, 'mcproxy_gate_backends'),
		snapshot_cache_age_seconds: metricValue(metricsText, 'mcproxy_snapshot_cache_age_seconds'),
		distributed_enabled: metricValue(metricsText, 'mcproxy_distributed_enabled'),
		distributed_healthy: metricValue(metricsText, 'mcproxy_distributed_healthy'),
		distributed_fail_closed: metricValue(metricsText, 'mcproxy_distributed_fail_closed'),
		distributed_fallbacks_total: metricValue(metricsText, 'mcproxy_distributed_fallbacks_total'),
		evaluation_latency_seconds_total: metricValue(metricsText, 'mcproxy_evaluation_latency_seconds_total'),
		evaluation_latency_seconds_count: metricValue(metricsText, 'mcproxy_evaluation_latency_seconds_count'),
		db_open_connections: metricValue(metricsText, 'mcproxy_db_open_connections'),
		db_in_use_connections: metricValue(metricsText, 'mcproxy_db_in_use_connections'),
		db_idle_connections: metricValue(metricsText, 'mcproxy_db_idle_connections'),
		db_wait_count_total: metricValue(metricsText, 'mcproxy_db_wait_count_total'),
		db_wait_duration_seconds_total: metricValue(metricsText, 'mcproxy_db_wait_duration_seconds_total'),
		store_stats_latency_seconds_total: metricValue(metricsText, 'mcproxy_store_stats_latency_seconds_total'),
		store_stats_latency_seconds_count: metricValue(metricsText, 'mcproxy_store_stats_latency_seconds_count'),
		audit_write_errors_total: metricValue(metricsText, 'mcproxy_audit_write_errors_total')
	};
	const policy = await jsonOrNull<Policy>(policyResponse);
	const geo = await jsonOrNull<GeoPolicy>(geoResponse);
	const rulesPayload = await jsonOrNull<{ items?: Rule[]; has_more?: boolean }>(rulesResponse);
	const allRules = rulesPayload?.items ?? [];
	const rules = member && member.role !== 'owner' && member.serverScope.length > 0
		? allRules.filter((rule) => typeof rule.server_id === 'number' && Number.isInteger(rule.server_id) && member.serverScope.includes(rule.server_id))
		: allRules;
	const serverControls = needsPolicyData ? Object.fromEntries(await Promise.all(servers.map(async (server) => {
		const [policyResult, geoResult, rulesResult] = await Promise.all([
			api(`/api/v1/policy/servers/${server.id}`, undefined, member?.username),
			api(`/api/v1/policy/geo?server_id=${server.id}`, undefined, member?.username),
			api(`/api/v1/rules?server_id=${server.id}&limit=100`, undefined, member?.username)
		]);
		let serverPolicy: Policy | null = null;
		const value = await jsonOrNull<Policy & { policy?: Policy | null }>(policyResult);
		if (value) {
			serverPolicy = value.policy ?? (value.id ? value : null);
		}
		const serverGeo = await jsonOrNull<GeoPolicy>(geoResult);
		const serverRules = (await jsonOrNull<{ items?: Rule[] }>(rulesResult))?.items ?? [];
		return [server.id, { policy: serverPolicy, geo: serverGeo, rules: serverRules }];
	}))) as Record<number, { policy: Policy | null; geo: GeoPolicy | null; rules: Rule[] }> : {};
	const serverPolicies = Object.fromEntries(Object.entries(serverControls).map(([id, value]) => [id, value.policy])) as Record<number, Policy | null>;
	const serverGeos = Object.fromEntries(Object.entries(serverControls).map(([id, value]) => [id, value.geo])) as Record<number, GeoPolicy | null>;
	const serverRules = Object.fromEntries(Object.entries(serverControls).map(([id, value]) => [id, value.rules])) as Record<number, Rule[]>;
	const healthTargets = view === 'servers' ? filteredServers : [];
	const serverHealth = view === 'servers' ? Object.fromEntries(await Promise.all(healthTargets.map(async (server) => {
		const response = await api(`/api/v1/servers/${server.id}/health`, undefined, member?.username);
		const health = (await jsonOrNull<ServerHealth>(response)) ?? { server_id: server.id, status: 'unknown', healthy: false } satisfies ServerHealth;
		return [server.id, health];
	}))) as Record<number, ServerHealth> : {};
	return { stats, metrics, servers, serverTotal: filteredServers.length, serverOffset, serverLimit, serverHasPrevious: serverOffset > 0, serverHasNext: serverOffset + serverLimit < filteredServers.length, distributionGroups, audits, incidentNotes, policy, geo, rules, ruleOffset, ruleLimit, ruleHasNext: rulesPayload?.has_more === true, serverPolicies, serverGeos, serverRules, serverHealth, view, serverSearch, serverEnabledFilter, auditEvent, auditActor, auditOffset, auditLimit, canEditGlobal: !member || (member.role === 'owner' || member.role === 'admin') && member.serverScope.length === 0, canCreateServers: !member || member.role === 'owner' || (member.role === 'admin' && member.serverScope.length === 0), canManageBungeeGuardTokens: canViewBungeeGuardTokens, canManageIncidents: member?.role === 'owner' || member?.role === 'admin' || member?.role === 'operator', returnTo: `${url.pathname}${url.search}`, auditHasNext, apiAvailable: statsResponse.ok && serversResponse.ok && auditsResponse.ok };
};

export const actions: Actions = {
	updateServerPolicy: async ({ request, cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin']); if (denied) return denied;
		const form = await request.formData();
		const serverID = String(form.get('server_id') ?? '');
		const scopeDenied = requireServerScope(cookies, serverID); if (scopeDenied) return scopeDenied;
		const number = (name: string) => Number(form.get(name) ?? 0);
		const payload = {
			ip_burst_10s: number('ip_burst_10s'),
			ip_burst_60s: number('ip_burst_60s'),
			name_burst_10s: number('name_burst_10s'),
			name_burst_60s: number('name_burst_60s'),
			deny_first: form.get('deny_first') === 'on'
		};
		if (!serverID || Object.values(payload).some((value) => typeof value === 'number' && (!Number.isInteger(value) || value < 0))) return fail(400, { error: message(cookies, 'msg.invalidServerPolicy') });
		const response = await api(`/api/v1/policy/servers/${encodeURIComponent(serverID)}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) }, panelActor(cookies));
		if (!response.ok) return fail(response.status, { error: await upstreamFailure(response, cookies) });
		return { success: message(cookies, 'msg.serverPolicyUpdated', { id: serverID }) };
	},
	resetServerPolicy: async ({ request, cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin']); if (denied) return denied;
		const form = await request.formData();
		const serverID = String(form.get('server_id') ?? '');
		if (!serverID) return fail(400, { error: message(cookies, 'msg.serverRequired') });
		const scopeDenied = requireServerScope(cookies, serverID); if (scopeDenied) return scopeDenied;
		const response = await api(`/api/v1/policy/servers/${encodeURIComponent(serverID)}`, { method: 'DELETE' }, panelActor(cookies));
		if (!response.ok) return fail(response.status, { error: await upstreamFailure(response, cookies) });
		return { success: message(cookies, 'msg.serverPolicyInherited', { id: serverID }) };
	},
	updatePolicy: async ({ request, cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin']); if (denied) return denied;
		const unrestricted = requireUnrestrictedMember(cookies); if (unrestricted) return unrestricted;
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
		if (Object.values(payload).some((value) => typeof value === 'number' && (!Number.isInteger(value) || value < 0))) return fail(400, { error: message(cookies, 'msg.thresholds') });
		const response = await api('/api/v1/policy/global', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) }, panelActor(cookies));
		if (!response.ok) return fail(response.status, { error: await upstreamFailure(response, cookies) });
		return { success: message(cookies, 'msg.globalPolicyUpdated') };
	},
	resetPolicy: async ({ cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin']); if (denied) return denied;
		const unrestricted = requireUnrestrictedMember(cookies); if (unrestricted) return unrestricted;
		const response = await api('/api/v1/policy/global', { method: 'DELETE' }, panelActor(cookies));
		if (!response.ok) return fail(response.status, { error: await upstreamFailure(response, cookies) });
		return { success: message(cookies, 'msg.globalPolicyReset') };
	},
	updateGeo: async ({ request, cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin']); if (denied) return denied;
		const form = await request.formData();
		const serverID = String(form.get('server_id') ?? '');
		if (!serverID) { const unrestricted = requireUnrestrictedMember(cookies); if (unrestricted) return unrestricted; }
		if (serverID) { const scopeDenied = requireServerScope(cookies, serverID); if (scopeDenied) return scopeDenied; }
		const countries = String(form.get('countries') ?? '').split(',').map((country) => country.trim().toUpperCase()).filter(Boolean);
		const query = serverID ? `?server_id=${encodeURIComponent(serverID)}` : '';
		const response = await api(`/api/v1/policy/geo${query}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ mode: String(form.get('mode') ?? 'disabled'), countries }) }, panelActor(cookies));
		if (!response.ok) return fail(response.status, { error: await upstreamFailure(response, cookies) });
		return { success: message(cookies, 'msg.geoUpdated') };
	},
	resetGeo: async ({ request, cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin']); if (denied) return denied;
		const form = await request.formData();
		const serverID = String(form.get('server_id') ?? '');
		if (!serverID) { const unrestricted = requireUnrestrictedMember(cookies); if (unrestricted) return unrestricted; }
		if (serverID) { const scopeDenied = requireServerScope(cookies, serverID); if (scopeDenied) return scopeDenied; }
		const query = serverID ? `?server_id=${encodeURIComponent(serverID)}` : '';
		const response = await api(`/api/v1/policy/geo${query}`, { method: 'DELETE' }, panelActor(cookies));
		if (!response.ok) return fail(response.status, { error: await upstreamFailure(response, cookies) });
		return { success: message(cookies, 'msg.geoReset') };
	},
	createRule: async ({ request, cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin']); if (denied) return denied;
		const form = await request.formData();
		const kind = String(form.get('kind') ?? '');
		const target = String(form.get('target') ?? '').trim();
		const serverID = String(form.get('server_id') ?? '');
		if (!target) return fail(400, { error: message(cookies, 'msg.ruleTarget') });
		const serverIDNumber: number | null = serverID ? Number(serverID) : null;
		if (serverIDNumber !== null && (!Number.isInteger(serverIDNumber) || serverIDNumber <= 0)) return fail(400, { error: message(cookies, 'msg.invalidServer') });
		if (serverIDNumber === null) { const unrestricted = requireUnrestrictedMember(cookies); if (unrestricted) return unrestricted; }
		if (serverID) { const scopeDenied = requireServerScope(cookies, serverID); if (scopeDenied) return scopeDenied; }
		const response = await api('/api/v1/rules/', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ kind, target, is_cidr: form.get('is_cidr') === 'on', reason: String(form.get('reason') ?? ''), ...(serverIDNumber !== null ? { server_id: serverIDNumber } : {}) }) }, panelActor(cookies));
		if (!response.ok) return fail(response.status, { error: await upstreamFailure(response, cookies) });
		return { success: message(cookies, 'msg.ruleCreated') };
	},
	removeRule: async ({ request, cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin']); if (denied) return denied;
		const form = await request.formData();
		const id = String(form.get('id') ?? '');
		const serverID = String(form.get('server_id') ?? '');
		if (!id) return fail(400, { error: message(cookies, 'msg.invalidServer') });
		const scopeDenied = await requireRuleScope(cookies, id, serverID); if (scopeDenied) return scopeDenied;
		const response = await api(`/api/v1/rules/${encodeURIComponent(id)}/`, { method: 'DELETE' }, panelActor(cookies));
		if (!response.ok) return fail(response.status, { error: await upstreamFailure(response, cookies) });
		return { success: message(cookies, 'msg.ruleDeleted') };
	},
	create: async ({ request, cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin']); if (denied) return denied;
		const unrestricted = requireUnrestrictedMember(cookies); if (unrestricted) return unrestricted;
		const form = await request.formData();
		const name = String(form.get('name') ?? '').trim();
		const upstream = String(form.get('upstream') ?? '').trim();
		const forwardingMode = String(form.get('forwarding_mode') ?? '').trim();
		const publicHost = String(form.get('public_host') ?? '').trim();
		const publicAddr = String(form.get('public_addr') ?? '').trim();
		const pingMotd = String(form.get('ping_motd') ?? '').trim();
		const priority = Number(form.get('priority') ?? 0);
		if (!name || !upstream) return fail(400, { error: message(cookies, 'msg.serverNameRequired') });
		if (!Number.isInteger(priority) || priority < 0 || priority > 1_000_000) return fail(400, { error: message(cookies, 'msg.priority') });
		const response = await api('/api/v1/servers/', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ name, upstream, forwarding_mode: forwardingMode, priority, ping_motd: pingMotd, ...(publicHost ? { public_host: publicHost } : {}), ...(publicAddr ? { public_addr: publicAddr } : {}) })
		}, panelActor(cookies));
		if (!response.ok) return fail(response.status, { error: await upstreamFailure(response, cookies) });
		return { success: message(cookies, 'msg.serverCreated') };
	},
	updateServer: async ({ request, cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin']); if (denied) return denied;
		const form = await request.formData();
		const id = String(form.get('id') ?? '');
		const name = String(form.get('name') ?? '').trim();
		const upstream = String(form.get('upstream') ?? '').trim();
		const forwardingMode = String(form.get('forwarding_mode') ?? '').trim();
		const publicHost = String(form.get('public_host') ?? '').trim();
		const publicAddr = String(form.get('public_addr') ?? '').trim();
		const pingMotd = String(form.get('ping_motd') ?? '').trim();
		const priority = Number(form.get('priority') ?? 0);
		if (!id || !name || !upstream) return fail(400, { error: message(cookies, 'msg.serverNameRequired') });
		const scopeDenied = requireServerScope(cookies, id); if (scopeDenied) return scopeDenied;
		if (!Number.isInteger(priority) || priority < 0 || priority > 1_000_000) return fail(400, { error: message(cookies, 'msg.priority') });
		const response = await api(`/api/v1/servers/${encodeURIComponent(id)}/`, {
			method: 'PUT',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ name, upstream, forwarding_mode: forwardingMode, public_host: publicHost, public_addr: publicAddr, ping_motd: pingMotd, priority })
		}, panelActor(cookies));
		if (!response.ok) return fail(response.status, { error: await upstreamFailure(response, cookies) });
		return { success: message(cookies, 'msg.serverUpdated', { id }) };
	},
	setEnabled: async ({ request, cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin', 'operator']); if (denied) return denied;
		const form = await request.formData();
		const id = String(form.get('id') ?? '');
		const scopeDenied = requireServerScope(cookies, id); if (scopeDenied) return scopeDenied;
		const enabled = form.get('enabled') === 'true';
		const response = await api(`/api/v1/servers/${encodeURIComponent(id)}/`, {
			method: 'PUT',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ enabled })
		}, panelActor(cookies));
		if (!response.ok) return fail(response.status, { error: await upstreamFailure(response, cookies) });
		return { success: message(cookies, enabled ? 'msg.serverEnabled' : 'msg.serverDisabled') };
	},
	rotateBungeeGuardToken: async ({ request, cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin']); if (denied) return denied;
		const form = await request.formData();
		const id = String(form.get('id') ?? '');
		if (!id) return fail(400, { error: message(cookies, 'msg.serverRequired') });
		const scopeDenied = requireServerScope(cookies, id); if (scopeDenied) return scopeDenied;
		const beforeResponse = await api(`/api/v1/servers/${encodeURIComponent(id)}/`, undefined, panelActor(cookies));
		if (!beforeResponse.ok) return fail(beforeResponse.status, { error: await upstreamFailure(beforeResponse, cookies) });
		const before = await jsonOrNull<Server>(beforeResponse);
		if (!before) return fail(502, { error: message(cookies, 'msg.invalidServer') });
		const response = await api(`/api/v1/servers/${encodeURIComponent(id)}/bungeeguard-token`, { method: 'POST' }, panelActor(cookies));
		if (!response.ok) return fail(response.status, { error: await upstreamFailure(response, cookies) });
		const rotated = await jsonOrNull<Server>(response);
		return { success: message(cookies, 'msg.tokenRotated', { id }), tokenRotation: { id, previous: before.bungeeguard_allowed_token ?? '', current: rotated?.bungeeguard_allowed_token ?? '' } };
	},
	remove: async ({ request, cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin']); if (denied) return denied;
		const form = await request.formData();
		const id = String(form.get('id') ?? '');
		const scopeDenied = requireServerScope(cookies, id); if (scopeDenied) return scopeDenied;
		const response = await api(`/api/v1/servers/${encodeURIComponent(id)}/`, { method: 'DELETE' }, panelActor(cookies));
		if (!response.ok) return fail(response.status, { error: await upstreamFailure(response, cookies) });
		return { success: message(cookies, 'msg.serverDeleted') };
	},
	createIncident: async ({ request, cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin', 'operator']); if (denied) return denied;
		const form = await request.formData();
		const incidentID = createIncidentNote(String(form.get('title') ?? ''), String(form.get('note') ?? ''), panelActor(cookies));
		if (!incidentID) return fail(400, { error: message(cookies, 'msg.incidentInvalid') });
		return { success: message(cookies, 'msg.incidentCreated') };
	},
	closeIncident: async ({ request, cookies }) => {
		const denied = requireRole(cookies, ['owner', 'admin', 'operator']); if (denied) return denied;
		const incidentID = String((await request.formData()).get('id') ?? '');
		if (!incidentID || !closeIncidentNote(incidentID, panelActor(cookies))) return fail(400, { error: message(cookies, 'msg.incidentAlreadyClosed') });
		return { success: message(cookies, 'msg.incidentClosed') };
	}
};
