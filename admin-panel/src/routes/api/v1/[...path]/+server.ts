import type { RequestHandler } from './$types';
import { createHmac } from 'node:crypto';
import { MCPROXY_API_URL } from '$app/env/private';
import { mcproxyAdminActorSecret, mcproxyAdminToken } from '#lib/server/secrets.js';
import { readSession, readSessionClaims, SESSION_COOKIE } from '#lib/server/session.js';
import { getMember, type Member } from '#lib/server/membership.js';

const hopByHop = new Set(['connection', 'host', 'content-length', 'transfer-encoding', 'cookie', 'x-admin-actor', 'x-admin-actor-signature']);
const MAX_PROXY_BODY_BYTES = 1 << 20;

async function readLimitedBody(request: Request) {
	if (request.method === 'GET' || request.method === 'HEAD') return new Uint8Array();
	const declaredLength = Number(request.headers.get('content-length') ?? '');
	if (Number.isFinite(declaredLength) && declaredLength > MAX_PROXY_BODY_BYTES) return null;
	if (!request.body) return new Uint8Array();
	const reader = request.body.getReader();
	const chunks: Uint8Array[] = [];
	let size = 0;
	try {
		while (true) {
			const { done, value } = await reader.read();
			if (done) break;
			if (!value) continue;
			size += value.byteLength;
			if (size > MAX_PROXY_BODY_BYTES) {
				await reader.cancel();
				return null;
			}
			chunks.push(value);
		}
	} finally {
		reader.releaseLock();
	}
	const body = new Uint8Array(size);
	let offset = 0;
	for (const chunk of chunks) {
		body.set(chunk, offset);
		offset += chunk.byteLength;
	}
	return body;
}

function allowedServer(member: Member, serverID: number) {
	return member.role === 'owner' || member.serverScope.length === 0 || member.serverScope.includes(serverID);
}

function serverIDFromPath(path: string) {
	const match = path.match(/^(?:servers|policy\/servers)\/(\d+)(?:\/|$)/);
	return match ? Number(match[1]) : null;
}

function serverIDFromQuery(url: URL) {
	const value = url.searchParams.get('server_id');
	if (!value) return null;
	const id = Number(value);
	return Number.isInteger(id) && id > 0 ? id : 0;
}

function serverIDsFromQuery(url: URL) {
	const values = url.searchParams.getAll('server_id');
	if (values.length === 0) return [];
	return values.map((value) => Number(value)).map((id) => Number.isInteger(id) && id > 0 ? id : 0);
}

function serverIDFromBody(path: string, method: string, body: Uint8Array) {
	if (method === 'GET' || method === 'HEAD' || (path !== 'rules' && path !== 'rules/')) return null;
	try {
		const payload = JSON.parse(new TextDecoder().decode(body)) as { server_id?: unknown };
		if (payload.server_id === undefined || payload.server_id === null || payload.server_id === '') return null;
		const id = Number(payload.server_id);
		return Number.isInteger(id) && id > 0 ? id : 0;
	} catch {
		return 0;
	}
}

function isGlobalMutation(path: string, method: string, url: URL, body: Uint8Array) {
	if (method === 'GET' || method === 'HEAD') return false;
	if (path === 'servers' || path === 'servers/') return true;
	if (path === 'policy/global' || path === 'policy/global/') return true;
	if (path === 'policy/geo' || path === 'policy/geo/') return serverIDFromQuery(url) === null;
	if (path === 'rules' || path === 'rules/') {
		try {
			const payload = JSON.parse(new TextDecoder().decode(body)) as { server_id?: unknown };
			return payload.server_id === undefined || payload.server_id === null;
		} catch {
			return true;
		}
	}
	// A rule detail mutation is scoped by looking up the rule's owning server in
	// verifyScopedRuleMutation. It has no server_id query parameter, so treating
	// the missing query as a global mutation would incorrectly deny scoped admins
	// from deleting rules they are allowed to manage.
	if (path.startsWith('rules/')) return false;
	return false;
}

function canManageServers(member: Member) {
	return member.role === 'owner' || member.role === 'admin';
}

function canManageTokens(member: Member) {
	return member.role === 'owner' || member.role === 'admin';
}

function canReadTokens(member: Member) {
	return canManageTokens(member);
}

// Keep the panel proxy narrower than the mcproxy API itself. In particular,
// do not let a catch-all route become a general authenticated upstream proxy
// when new API endpoints are added later or when a path contains dot segments.
function isSupportedPanelPath(path: string) {
	return /^(?:stats|audits|servers(?:\/\d+(?:\/bungeeguard-token|\/health)?)?|policy\/(?:global|geo|servers\/\d+)|rules(?:\/\d+)?)$/.test(path);
}

function mutationAllowed(path: string, method: string, body: Uint8Array, member: Member) {
	if (method === 'GET' || method === 'HEAD') return true;
	if (path === 'servers' || path === 'servers/') return method === 'POST' && canManageServers(member);
	if (/^servers\/\d+\/bungeeguard-token\/?$/.test(path)) return method === 'POST' && canManageTokens(member);
	if (/^servers\/\d+\/?$/.test(path)) {
		if (method !== 'PUT' && method !== 'DELETE') return false;
		if (canManageServers(member)) return true;
		if (member.role !== 'operator' || method !== 'PUT') return false;
		try {
			const payload = JSON.parse(new TextDecoder().decode(body)) as Record<string, unknown>;
			return Object.keys(payload).length === 1 && typeof payload.enabled === 'boolean';
		} catch {
			return false;
		}
	}
	if (path === 'policy/global' || path === 'policy/global/') return (method === 'PUT' || method === 'DELETE') && canManageServers(member) && member.serverScope.length === 0;
	if (path === 'policy/geo' || path === 'policy/geo/') return (method === 'PUT' || method === 'DELETE') && canManageServers(member);
	if (path === 'rules' || path === 'rules/') return method === 'POST' && canManageServers(member);
	if (path.startsWith('rules/')) return method === 'DELETE' && canManageServers(member);
	return false;
}

function scopedReadDenied(path: string, method: string, url: URL, member: Member) {
	if ((method !== 'GET' && method !== 'HEAD') || member.role === 'owner' || member.serverScope.length === 0) return false;
	if (path === 'policy/global' || path === 'policy/global/') return true;
	if (path === 'rules' || path === 'rules/') return !url.searchParams.has('server_id');
	return (path === 'policy/geo' || path === 'policy/geo/') && !url.searchParams.has('server_id');
}

function redactServerToken(item: Record<string, unknown>) {
	const copy = { ...item };
	delete copy.bungeeguard_allowed_token;
	return copy;
}

async function filterScopedList(response: Response, member: Member, path: string) {
	if (!response.ok) return response;
	const serverList = path === 'servers' || path === 'servers/';
	const serverDetail = /^servers\/\d+\/?$/.test(path);
	const needsTokenRedaction = !canReadTokens(member) && (serverList || serverDetail);
	const needsScopeFiltering = member.role !== 'owner' && member.serverScope.length > 0 && (serverList || path === 'rules' || path === 'rules/' || path === 'audits');
	if (!needsTokenRedaction && !needsScopeFiltering) return response;
	try {
		const payload = await response.json() as { items?: Array<Record<string, unknown>> } & Record<string, unknown>;
		if (serverDetail) {
			return new Response(JSON.stringify(needsTokenRedaction ? redactServerToken(payload) : payload), { status: response.status, headers: { 'content-type': 'application/json; charset=utf-8' } });
		}
		if (!Array.isArray(payload.items)) return new Response(JSON.stringify(payload), { status: response.status, headers: { 'content-type': 'application/json; charset=utf-8' } });
		let items = payload.items;
		if (needsScopeFiltering) {
			items = items.filter((item) => {
				const serverID = path === 'rules' || path === 'rules/' || path === 'audits' ? Number(item.server_id) : Number(item.id ?? item.server_id);
				return Number.isInteger(serverID) && member.serverScope.includes(serverID);
			});
		}
		if (needsTokenRedaction) items = items.map(redactServerToken);
		return new Response(JSON.stringify({ ...payload, items }), { status: response.status, headers: { 'content-type': 'application/json; charset=utf-8' } });
	} catch {
		return new Response('Invalid upstream response', { status: 502 });
	}
}

async function verifyScopedRuleMutation(fetcher: typeof fetch, baseURL: string, path: string, member: Member, adminToken: string) {
	if (member.role === 'owner' || member.serverScope.length === 0 || !path.startsWith('rules/')) return true;
	const match = path.match(/^rules\/(\d+)$/);
	if (!match) return false;
	try {
		const pageSize = 100;
		for (let offset = 0; offset <= 1_000_000; offset += pageSize) {
			const response = await fetcher(`${baseURL}/api/v1/rules/?limit=${pageSize}&offset=${offset}`, {
				headers: { Authorization: `Bearer ${adminToken}` },
				signal: AbortSignal.timeout(8_000)
			});
			if (!response.ok) return false;
			const payload = await response.json() as { items?: Array<{ id?: unknown; server_id?: unknown }>; has_more?: boolean };
			const item = payload.items?.find((candidate) => Number(candidate.id) === Number(match[1]));
			if (item) {
				const serverID = Number(item.server_id);
				return Number.isInteger(serverID) && allowedServer(member, serverID);
			}
			if (payload.has_more !== true) return false;
		}
		return false;
	} catch {
		return false;
	}
}

export const fallback: RequestHandler = async ({ params, request, fetch, url, cookies }) => {
	const adminToken = mcproxyAdminToken();
	if (!MCPROXY_API_URL || !adminToken) {
		return new Response('Admin panel is not configured', { status: 503 });
	}
	const claims = readSessionClaims(cookies.get(SESSION_COOKIE));
	const member = claims?.userId ? getMember(claims.userId) : null;
	if (!member || member.username !== claims?.username) return new Response('Admin authentication required', { status: 401 });
	const path = params.path.replace(/^\/+|\/+$/g, '');
	if (!isSupportedPanelPath(path)) return new Response('Panel API route not allowed', { status: 404 });
	const upstreamPath = `${path}${params.path.endsWith('/') ? '/' : ''}`;
	const body = await readLimitedBody(request);
	if (body === null) return new Response('request body too large (maximum 1 MiB)', { status: 413 });
	const pathServerID = serverIDFromPath(path);
	const queryServerID = serverIDFromQuery(url);
	const queryServerIDs = serverIDsFromQuery(url);
	const bodyServerID = serverIDFromBody(path, request.method, body);
	const scopedServerID = pathServerID ?? (queryServerID && queryServerID > 0 ? queryServerID : null);
	if (scopedServerID !== null && !allowedServer(member, scopedServerID)) return new Response('Server scope denied', { status: 403 });
	if (queryServerIDs.some((id) => id === 0)) return new Response('Invalid server scope', { status: 400 });
	if (queryServerIDs.some((id) => !allowedServer(member, id))) return new Response('Server scope denied', { status: 403 });
	if (bodyServerID === 0) return new Response('Invalid server scope', { status: 400 });
	if (bodyServerID !== null && !allowedServer(member, bodyServerID)) return new Response('Server scope denied', { status: 403 });
	if (scopedReadDenied(path, request.method, url, member)) return new Response('Global read requires an unrestricted administrator', { status: 403 });
	if (!mutationAllowed(path, request.method, body, member)) return new Response('Operation not allowed for this role', { status: 403 });
	if (isGlobalMutation(path, request.method, url, body) && member.role !== 'owner' && member.serverScope.length > 0) return new Response('Global operation requires an unrestricted administrator', { status: 403 });
	if (!(await verifyScopedRuleMutation(fetch, MCPROXY_API_URL, path, member, adminToken))) return new Response('Rule scope denied', { status: 403 });
	const headers = new Headers(request.headers);
	for (const name of hopByHop) headers.delete(name);
	headers.set('Authorization', `Bearer ${adminToken}`);
	const actor = readSession(cookies.get(SESSION_COOKIE));
	const actorSecret = mcproxyAdminActorSecret();
	if (actor && actorSecret) {
		headers.set('X-Admin-Actor', actor);
		headers.set('X-Admin-Actor-Signature', createHmac('sha256', actorSecret).update(actor).digest('hex'));
	}
	const upstreamSearch = new URLSearchParams(url.search);
	if (path === 'audits' && member.role !== 'owner' && member.serverScope.length > 0 && !upstreamSearch.has('server_id')) {
		for (const serverID of member.serverScope) upstreamSearch.append('server_id', String(serverID));
	}

	let response: Response;
	try {
		const search = upstreamSearch.toString();
		response = await fetch(`${MCPROXY_API_URL}/api/v1/${upstreamPath}${search ? `?${search}` : ''}`, {
			method: request.method,
			headers,
			body: request.method === 'GET' || request.method === 'HEAD' ? undefined : body,
			signal: AbortSignal.timeout(15_000)
		});
	} catch {
		return new Response('mcproxy API upstream timed out or is unavailable', { status: 504 });
	}
	const filtered = await filterScopedList(response, member, path);

	const outgoing = new Headers(filtered.headers);
	outgoing.delete('set-cookie');
	return new Response(filtered.body, { status: filtered.status, headers: outgoing });
};

export const GET = fallback;
export const POST = fallback;
export const PUT = fallback;
export const DELETE = fallback;
export const PATCH = fallback;
