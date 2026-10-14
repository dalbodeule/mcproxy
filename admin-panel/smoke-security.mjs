const base = 'http://127.0.0.1:3000';
const common = { 'X-Forwarded-Proto': 'http' };

/** @param {Response} response */
function cookieFrom(response) {
	const values = response.headers.getSetCookie?.() ?? [];
	const value = values[0] ?? response.headers.get('set-cookie') ?? '';
	return value.split(';')[0];
}

let response = await fetch(`${base}/login`, {
	method: 'POST',
	redirect: 'manual',
	headers: { ...common, Origin: base, Accept: 'text/html', 'Content-Type': 'application/x-www-form-urlencoded' },
	body: new URLSearchParams({ username: 'admin', password: 'wrong-password' })
});
if (response.status !== 401) throw new Error(`invalid login did not return an authentication error: ${response.status}`);

response = await fetch(`${base}/login?returnTo=%2Faccount`, {
	method: 'POST',
	redirect: 'manual',
	headers: { ...common, Origin: base, 'Content-Type': 'application/x-www-form-urlencoded' },
	body: new URLSearchParams({ username: 'admin', password: 'smoke-password-1234', returnTo: '/account' })
});
if (![200, 303].includes(response.status)) throw new Error(`login failed: ${response.status}`);
if (response.status === 303 && response.headers.get('location') !== '/account') throw new Error(`safe login return target was not honored: ${response.headers.get('location')}`);
if (response.headers.get('cache-control') !== 'no-store') throw new Error(`sensitive response is cacheable: ${response.headers.get('cache-control')}`);
const cookie = cookieFrom(response);
if (!cookie) throw new Error('admin session cookie missing');

response = await fetch(`${base}/api/v1/stats`, {
	method: 'POST',
	headers: { ...common, Origin: 'http://evil.example', Cookie: cookie, 'Content-Type': 'application/json' },
	body: '{}'
});
if (response.status !== 403) throw new Error(`cross-origin API mutation was not blocked: ${response.status}`);
response = await fetch(`${base}/api/v1/stats`, {
	method: 'POST',
	headers: { ...common, Cookie: cookie, 'Content-Type': 'application/json' },
	body: '{}'
});
if (response.status !== 403) throw new Error(`origin-less API mutation was not blocked: ${response.status}`);
response = await fetch(`${base}/contrast`, {
	method: 'POST',
	redirect: 'manual',
	headers: { ...common, Origin: base, 'Content-Type': 'application/x-www-form-urlencoded' },
	body: new URLSearchParams({ contrast: 'high' })
});
if (response.status !== 303 || !(response.headers.get('set-cookie') ?? '').includes('mcproxy_contrast=high')) throw new Error(`high contrast preference was not stored: ${response.status}`);
response = await fetch(`${base}/api/v1/evaluate`, { headers: { ...common, Cookie: cookie } });
if (response.status !== 404) throw new Error(`non-panel upstream route was not blocked: ${response.status}`);
response = await fetch(`${base}/api/v1/servers/`, {
	method: 'POST',
	headers: { ...common, Origin: base, Cookie: cookie, 'Content-Type': 'application/json' },
	body: JSON.stringify({ name: 'x'.repeat(1_100_000) })
});
if (response.status !== 413) throw new Error(`oversized panel proxy body was not blocked: ${response.status}`);
console.log('Admin panel CSRF and upstream allowlist smoke test OK');
