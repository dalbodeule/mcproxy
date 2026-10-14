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
	headers: { ...common, Origin: base, 'Content-Type': 'application/x-www-form-urlencoded' },
	body: new URLSearchParams({ username: 'admin', password: 'smoke-password-1234' })
});
if (![200, 303].includes(response.status)) throw new Error(`login failed: ${response.status}`);
const cookie = cookieFrom(response);
if (!cookie) throw new Error('admin session cookie missing');

response = await fetch(`${base}/`, { headers: { ...common, Cookie: cookie } });
const body = await response.text();
if (response.status !== 200) throw new Error(`dashboard failed: ${response.status}`);
if (!body.includes('Immediate attention')) throw new Error('dashboard attention heading is missing');
if (!body.includes('aria-live="polite"')) throw new Error('dashboard attention region is not announced');
console.log('Admin panel dashboard alert smoke test OK');
