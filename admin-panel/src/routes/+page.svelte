<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import type { ActionData, PageData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();
	let refreshing = $state(false);

	async function refresh() {
		refreshing = true;
		await invalidateAll();
		refreshing = false;
	}

	function duration(seconds: number) {
		const hours = Math.floor(seconds / 3600);
		const minutes = Math.floor((seconds % 3600) / 60);
		return `${hours}h ${minutes}m`;
	}
</script>

<svelte:head>
	<title>mcproxy Admin</title>
	<meta name="description" content="mcproxy security proxy operations panel" />
</svelte:head>

<main class="min-h-screen bg-slate-950">
	<div class="mx-auto max-w-7xl px-6 py-8 lg:px-10">
		<header class="mb-8 flex flex-wrap items-end justify-between gap-4">
			<div>
				<p class="mb-2 text-sm font-semibold uppercase tracking-[0.25em] text-cyan-400">mcproxy control plane</p>
				<h1 class="text-3xl font-bold tracking-tight text-white">Security operations</h1>
				<p class="mt-2 text-slate-400">Monitor policy state and backend routing from the isolated admin server.</p>
			</div>
			<div class="flex gap-2"><button class="rounded-lg border border-slate-700 px-4 py-2 text-sm text-slate-200 transition hover:border-cyan-400 hover:text-cyan-300 disabled:opacity-50" onclick={refresh} disabled={refreshing}>{refreshing ? 'Refreshing…' : 'Refresh'}</button><form method="POST" action="/logout"><button class="rounded-lg border border-slate-700 px-4 py-2 text-sm text-slate-400 hover:border-red-400 hover:text-red-300">Sign out</button></form></div>
		</header>

		{#if !data.apiAvailable}
			<div class="mb-6 rounded-xl border border-amber-500/40 bg-amber-500/10 p-4 text-amber-200">mcproxy API is unavailable. Check MCPROXY_API_URL and the server-side token.</div>
		{/if}

		<section class="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
			{#each [
				['Uptime', data.stats ? duration(data.stats.uptime_seconds) : '—'],
				['Servers', data.stats?.servers ?? '—'],
				['Policies', data.stats?.policies ?? '—'],
				['Rules', data.stats?.rules ?? '—'],
				['Blocked', data.stats?.blocks ?? '—']
			] as card}
				<div class="rounded-xl border border-slate-800 bg-slate-900/80 p-5 shadow-lg shadow-black/10">
					<p class="text-sm text-slate-400">{card[0]}</p>
					<p class="mt-2 text-2xl font-semibold text-white">{card[1]}</p>
				</div>
			{/each}
		</section>

		<section class="mt-8 grid gap-6 xl:grid-cols-2">
			<form method="POST" action="?/updatePolicy" class="rounded-xl border border-slate-800 bg-slate-900/80 p-5">
				<div class="mb-4"><h2 class="font-semibold text-white">Global connection policy</h2><p class="mt-1 text-sm text-slate-400">Thresholds are evaluated before backend connection.</p></div>
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="text-xs text-slate-400">IP / 10 seconds<input name="ip_burst_10s" type="number" min="0" value={data.policy?.ip_burst_10s ?? 0} class="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white" /></label>
					<label class="text-xs text-slate-400">IP / 60 seconds<input name="ip_burst_60s" type="number" min="0" value={data.policy?.ip_burst_60s ?? 0} class="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white" /></label>
					<label class="text-xs text-slate-400">Name / 10 seconds<input name="name_burst_10s" type="number" min="0" value={data.policy?.name_burst_10s ?? 0} class="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white" /></label>
					<label class="text-xs text-slate-400">Name / 60 seconds<input name="name_burst_60s" type="number" min="0" value={data.policy?.name_burst_60s ?? 0} class="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white" /></label>
				</div>
				<label class="mt-3 flex items-center gap-2 text-sm text-slate-300"><input name="deny_first" type="checkbox" checked={data.policy?.deny_first ?? false} /> Deny rules take precedence</label>
				<label class="mt-3 block text-xs text-slate-400">Kick message<textarea name="kick_message" rows="2" class="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white">{data.policy?.kick_message ?? ''}</textarea></label>
				<div class="mt-4 flex flex-wrap gap-2"><button class="rounded-lg bg-cyan-400 px-4 py-2 text-sm font-semibold text-slate-950 hover:bg-cyan-300">Save policy</button><button formaction="?/resetPolicy" formmethod="POST" class="rounded-lg border border-amber-500/50 px-4 py-2 text-sm text-amber-300 hover:bg-amber-400/10">Reset defaults</button></div>
			</form>

			<form method="POST" action="?/updateGeo" class="rounded-xl border border-slate-800 bg-slate-900/80 p-5">
				<div class="mb-4"><h2 class="font-semibold text-white">Geo policy</h2><p class="mt-1 text-sm text-slate-400">Country codes are normalized to ISO uppercase.</p></div>
				<label class="block text-xs text-slate-400">Mode<select name="mode" class="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white"><option value="disabled" selected={data.geo?.mode === 'disabled'}>Disabled</option><option value="allow" selected={data.geo?.mode === 'allow'}>Allowlist</option><option value="deny" selected={data.geo?.mode === 'deny'}>Denylist</option></select></label>
				<label class="mt-3 block text-xs text-slate-400">Countries<input name="countries" value={data.geo?.countries?.join(', ') ?? ''} placeholder="KR, JP, US" class="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white placeholder:text-slate-600" /></label>
				<div class="mt-4 flex flex-wrap gap-2"><button class="rounded-lg bg-cyan-400 px-4 py-2 text-sm font-semibold text-slate-950 hover:bg-cyan-300">Save Geo policy</button><button formaction="?/resetGeo" formmethod="POST" class="rounded-lg border border-amber-500/50 px-4 py-2 text-sm text-amber-300 hover:bg-amber-400/10">Disable Geo</button></div>
			</form>
		</section>

		<section class="mt-8 overflow-hidden rounded-xl border border-slate-800 bg-slate-900/80">
			<div class="border-b border-slate-800 px-5 py-4"><h2 class="font-semibold text-white">Allow / deny rules</h2><p class="mt-1 text-sm text-slate-400">Global IP and nickname rules.</p></div>
			<form method="POST" action="?/createRule" class="grid gap-2 border-b border-slate-800 bg-slate-950/40 p-5 md:grid-cols-4">
				<select name="kind" class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm text-white"><option value="ip-allow">IP allow</option><option value="ip-deny">IP deny</option><option value="name-allow">Name allow</option><option value="name-deny">Name deny</option></select>
				<input name="target" required placeholder="IP, CIDR, or nickname" class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm text-white placeholder:text-slate-600" />
				<input name="reason" placeholder="Reason (optional)" class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm text-white placeholder:text-slate-600" />
				<button class="rounded-lg bg-cyan-400 px-4 py-2 text-sm font-semibold text-slate-950 hover:bg-cyan-300">Add rule</button>
			</form>
			<div class="divide-y divide-slate-800">
				{#each data.rules as rule}
					<div class="flex flex-wrap items-center justify-between gap-3 px-5 py-3 text-sm"><div><span class="rounded bg-slate-800 px-2 py-1 text-xs text-cyan-300">{rule.kind}</span><span class="ml-3 font-mono text-slate-200">{rule.target}</span>{#if rule.reason}<span class="ml-3 text-slate-500">{rule.reason}</span>{/if}</div><form method="POST" action="?/removeRule"><input type="hidden" name="id" value={rule.id} /><button class="rounded border border-red-900 px-2 py-1 text-xs text-red-300 hover:border-red-400">Delete</button></form></div>
				{:else}<div class="px-5 py-8 text-center text-sm text-slate-500">No rules configured.</div>{/each}
			</div>
		</section>

		<section class="mt-8 rounded-xl border border-slate-800 bg-slate-900/80 p-5">
			<div class="mb-4"><h2 class="font-semibold text-white">Server-specific policies</h2><p class="mt-1 text-sm text-slate-400">Override global thresholds for individual backends.</p></div>
			<div class="grid gap-4 lg:grid-cols-2">
				{#each data.servers as server}
					{@const policy = data.serverPolicies[server.id]}
					<form method="POST" action="?/updateServerPolicy" class="rounded-lg border border-slate-800 bg-slate-950/50 p-4">
						<div class="mb-3 flex items-center justify-between"><h3 class="font-medium text-white">{server.name}</h3><input type="hidden" name="server_id" value={server.id} /><span class="text-xs text-slate-500">{policy ? 'Override' : 'Inherited'}</span></div>
						<div class="grid gap-2 sm:grid-cols-2">
							<input name="ip_burst_10s" type="number" min="0" value={policy?.ip_burst_10s ?? data.policy?.ip_burst_10s ?? 0} placeholder="IP / 10s" class="rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-sm text-white" />
							<input name="ip_burst_60s" type="number" min="0" value={policy?.ip_burst_60s ?? data.policy?.ip_burst_60s ?? 0} placeholder="IP / 60s" class="rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-sm text-white" />
							<input name="name_burst_10s" type="number" min="0" value={policy?.name_burst_10s ?? data.policy?.name_burst_10s ?? 0} placeholder="Name / 10s" class="rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-sm text-white" />
							<input name="name_burst_60s" type="number" min="0" value={policy?.name_burst_60s ?? data.policy?.name_burst_60s ?? 0} placeholder="Name / 60s" class="rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-sm text-white" />
						</div>
						<label class="mt-3 flex items-center gap-2 text-sm text-slate-300"><input name="deny_first" type="checkbox" checked={policy?.deny_first ?? data.policy?.deny_first ?? false} /> Deny first</label>
						<div class="mt-3 flex flex-wrap gap-2"><button class="rounded border border-cyan-500/50 px-3 py-1.5 text-xs text-cyan-300 hover:bg-cyan-400/10">Save server policy</button><button formaction="?/resetServerPolicy" formmethod="POST" class="rounded border border-amber-500/50 px-3 py-1.5 text-xs text-amber-300 hover:bg-amber-400/10">Inherit global</button></div>
					</form>
				{:else}<p class="text-sm text-slate-500">Add a backend server to configure an override.</p>{/each}
			</div>
		</section>

		<section class="mt-8 rounded-xl border border-slate-800 bg-slate-900/80 p-5">
			<div class="mb-4"><h2 class="font-semibold text-white">Server-specific Geo and rules</h2><p class="mt-1 text-sm text-slate-400">These overrides apply only after the matching backend route is selected.</p></div>
			<div class="grid gap-4 lg:grid-cols-2">
				{#each data.servers as server}
					{@const serverGeo = data.serverGeos[server.id]}
					{@const scopedRules = data.serverRules[server.id] ?? []}
					<div class="rounded-lg border border-slate-800 bg-slate-950/50 p-4">
						<h3 class="mb-3 font-medium text-white">{server.name}</h3>
						<form method="POST" action="?/updateGeo" class="space-y-2">
							<input type="hidden" name="server_id" value={server.id} />
							<label class="block text-xs text-slate-400">Geo mode<select name="mode" class="mt-1 w-full rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-sm text-white"><option value="disabled" selected={serverGeo?.mode === 'disabled' || !serverGeo}>Disabled</option><option value="allow" selected={serverGeo?.mode === 'allow'}>Allowlist</option><option value="deny" selected={serverGeo?.mode === 'deny'}>Denylist</option></select></label>
							<label class="block text-xs text-slate-400">Countries<input name="countries" value={serverGeo?.countries?.join(', ') ?? ''} placeholder="KR, JP, US" class="mt-1 w-full rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-sm text-white placeholder:text-slate-600" /></label>
							<div class="flex flex-wrap gap-2"><button class="rounded border border-cyan-500/50 px-3 py-1.5 text-xs text-cyan-300 hover:bg-cyan-400/10">Save Geo</button><button formaction="?/resetGeo" formmethod="POST" class="rounded border border-amber-500/50 px-3 py-1.5 text-xs text-amber-300 hover:bg-amber-400/10">Reset Geo</button></div>
						</form>
						<form method="POST" action="?/createRule" class="mt-4 grid gap-2 sm:grid-cols-3">
							<input type="hidden" name="server_id" value={server.id} />
							<select name="kind" class="rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-xs text-white"><option value="ip-allow">IP allow</option><option value="ip-deny">IP deny</option><option value="name-allow">Name allow</option><option value="name-deny">Name deny</option></select>
							<input name="target" required placeholder="IP/CIDR/name" class="rounded border border-slate-700 bg-slate-900 px-2 py-1.5 text-xs text-white placeholder:text-slate-600" />
							<button class="rounded border border-cyan-500/50 px-3 py-1.5 text-xs text-cyan-300 hover:bg-cyan-400/10">Add rule</button>
						</form>
						<div class="mt-3 space-y-1">
							{#each scopedRules as rule}
								<div class="flex items-center justify-between gap-2 text-xs"><span><span class="text-cyan-300">{rule.kind}</span> <span class="font-mono text-slate-300">{rule.target}</span></span><form method="POST" action="?/removeRule"><input type="hidden" name="id" value={rule.id} /><button class="text-red-300 hover:text-red-200">Delete</button></form></div>
							{:else}<p class="text-xs text-slate-600">No server-specific rules.</p>{/each}
						</div>
					</div>
				{:else}<p class="text-sm text-slate-500">Add a backend server to configure scoped rules.</p>{/each}
			</div>
		</section>

		<section class="mt-8 overflow-hidden rounded-xl border border-slate-800 bg-slate-900/80">
			<div class="flex flex-wrap items-center justify-between gap-4 border-b border-slate-800 px-5 py-4">
				<div>
					<h2 class="font-semibold text-white">Backend servers</h2>
					<p class="mt-1 text-sm text-slate-400">Hostname routes and upstream targets.</p>
				</div>
				<span class="rounded-full bg-cyan-400/10 px-3 py-1 text-xs font-medium text-cyan-300">{data.servers.length} configured</span>
			</div>
			<form method="POST" action="?/create" class="grid gap-2 border-b border-slate-800 bg-slate-950/40 p-5 md:grid-cols-6">
				<input name="name" required placeholder="Name" class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm outline-none placeholder:text-slate-600 focus:border-cyan-400" />
				<input name="upstream" required placeholder="127.0.0.1:25566" class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 font-mono text-sm outline-none placeholder:text-slate-600 focus:border-cyan-400" />
				<input name="public_host" placeholder="play.example.com (optional)" class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm outline-none placeholder:text-slate-600 focus:border-cyan-400" />
				<input name="public_addr" placeholder="play.example.com:25565 (optional)" class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 font-mono text-sm outline-none placeholder:text-slate-600 focus:border-cyan-400" />
				<select name="forwarding_mode" class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm text-white" aria-label="Forwarding mode"><option value="">Global fallback</option><option value="none">None</option><option value="legacy">Legacy</option><option value="bungeeguard">BungeeGuard</option><option value="velocity">Velocity</option></select>
				<input name="ping_motd" maxlength="256" placeholder="Ping MOTD (optional)" class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm outline-none placeholder:text-slate-600 focus:border-cyan-400" />
				<input name="priority" type="number" min="0" value="0" placeholder="Priority" class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 text-sm outline-none placeholder:text-slate-600 focus:border-cyan-400" />
				<button class="rounded-lg bg-cyan-400 px-4 py-2 text-sm font-semibold text-slate-950 transition hover:bg-cyan-300">Add server</button>
			</form>
			{#if form?.error}<div class="border-b border-red-500/30 bg-red-500/10 px-5 py-3 text-sm text-red-300">{form.error}</div>{/if}
			{#if form?.success}<div class="border-b border-emerald-500/30 bg-emerald-500/10 px-5 py-3 text-sm text-emerald-300">{form.success}</div>{/if}
			<div class="overflow-x-auto">
				<table class="w-full min-w-[980px] text-left text-sm">
					<thead class="bg-slate-950/70 text-xs uppercase tracking-wider text-slate-500">
						<tr><th class="px-5 py-3">Name</th><th class="px-5 py-3">Public route</th><th class="px-5 py-3">Upstream</th><th class="px-5 py-3">Forwarding</th><th class="px-5 py-3">Priority</th><th class="px-5 py-3">Status</th><th class="px-5 py-3">BungeeGuard allowed-tokens entry</th><th class="px-5 py-3">Actions</th></tr>
					</thead>
					<tbody class="divide-y divide-slate-800">
						{#each data.servers as server}
							<tr class="text-slate-300">
								<td class="px-5 py-4 font-medium text-white">{server.name}</td>
								<td class="px-5 py-4">{server.public_host || server.public_addr || 'default route'}</td>
								<td class="px-5 py-4 font-mono text-xs text-slate-400">{server.upstream}</td>
								<td class="px-5 py-4 text-xs uppercase text-cyan-300">{server.forwarding_mode || 'global'}</td>
								<td class="px-5 py-4">{server.priority}</td>
								<td class="px-5 py-4"><span class={server.enabled ? 'text-emerald-400' : 'text-slate-500'}>{server.enabled ? 'Enabled' : 'Disabled'}</span></td>
								<td class="px-5 py-4"><code class="break-all text-[11px] text-amber-300">{server.bungeeguard_allowed_token}</code><form class="mt-2" method="POST" action="?/rotateBungeeGuardToken" onsubmit={(event) => { if (!confirm(`Rotate the BungeeGuard token for ${server.name}? Update the backend allowed-tokens list and reload immediately.`)) event.preventDefault(); }}><input type="hidden" name="id" value={server.id} /><button class="text-[11px] text-amber-200 hover:text-amber-100">Regenerate token</button></form></td>
								<td class="flex gap-2 px-5 py-3">
									<form method="POST" action="?/setEnabled"><input type="hidden" name="id" value={server.id} /><input type="hidden" name="enabled" value={String(!server.enabled)} /><button class="rounded border border-slate-700 px-2 py-1 text-xs hover:border-cyan-400">{server.enabled ? 'Disable' : 'Enable'}</button></form>
									<form method="POST" action="?/remove" onsubmit={(event) => { if (!confirm(`Delete ${server.name}?`)) event.preventDefault(); }}><input type="hidden" name="id" value={server.id} /><button class="rounded border border-red-900 px-2 py-1 text-xs text-red-300 hover:border-red-400">Delete</button></form>
								</td>
							</tr>
						{:else}
							<tr><td class="px-5 py-8 text-center text-slate-500" colspan="8">No backend servers configured.</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
			<div class="border-t border-slate-800 bg-slate-950/30 p-5">
				<p class="mb-3 text-xs uppercase tracking-wider text-slate-500">Edit routing</p>
				<div class="grid gap-3 lg:grid-cols-2">
					{#each data.servers as server}
						<form method="POST" action="?/updateServer" class="grid gap-2 rounded-lg border border-slate-800 bg-slate-900/70 p-3 sm:grid-cols-2">
							<input type="hidden" name="id" value={server.id} />
							<input name="name" required value={server.name} aria-label={`${server.name} name`} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm text-white" />
							<input name="upstream" required value={server.upstream} aria-label={`${server.name} upstream`} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 font-mono text-xs text-white" />
							<input name="public_host" value={server.public_host ?? ''} placeholder="Public hostname" aria-label={`${server.name} public hostname`} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm text-white" />
							<input name="public_addr" value={server.public_addr ?? ''} placeholder="Public host:port" aria-label={`${server.name} public address`} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 font-mono text-xs text-white" />
							<select name="forwarding_mode" value={server.forwarding_mode ?? ''} aria-label={`${server.name} forwarding mode`} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-xs text-white"><option value="">Global fallback</option><option value="none">None</option><option value="legacy">Legacy</option><option value="bungeeguard">BungeeGuard</option><option value="velocity">Velocity</option></select>
							<input name="ping_motd" maxlength="256" value={server.ping_motd ?? ''} placeholder="Ping MOTD" aria-label={`${server.name} ping MOTD`} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm text-white" />
							<input name="priority" type="number" min="0" value={server.priority} aria-label={`${server.name} priority`} class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm text-white" />
							<button class="rounded border border-cyan-500/50 px-3 py-1.5 text-xs text-cyan-300 hover:bg-cyan-400/10">Save {server.name}</button>
						</form>
					{/each}
				</div>
			</div>
		</section>

		<section class="mt-8 overflow-hidden rounded-xl border border-slate-800 bg-slate-900/80">
			<div class="border-b border-slate-800 px-5 py-4">
				<div class="flex flex-wrap items-end justify-between gap-3"><div><h2 class="font-semibold text-white">Recent audit events</h2><p class="mt-1 text-sm text-slate-400">Latest policy and connection decisions.</p></div><form method="GET" class="flex gap-2"><select name="event" class="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-xs text-slate-200"><option value="" selected={!data.auditEvent}>All events</option><option value="connect.blocked" selected={data.auditEvent === 'connect.blocked'}>Blocked connections</option><option value="policy.update" selected={data.auditEvent === 'policy.update'}>Policy changes</option><option value="policy.reset" selected={data.auditEvent === 'policy.reset'}>Policy resets</option><option value="server.create" selected={data.auditEvent === 'server.create'}>Server changes</option><option value="rule.create" selected={data.auditEvent === 'rule.create'}>Rule changes</option></select><button class="rounded border border-slate-700 px-2 py-1.5 text-xs text-slate-300 hover:border-cyan-400">Filter</button></form></div>
			</div>
			<div class="divide-y divide-slate-800">
				{#each data.audits as audit}
					<div class="flex flex-wrap items-center justify-between gap-3 px-5 py-4 text-sm">
						<div><span class="font-medium text-white">{audit.event}</span>{#if audit.actor}<span class="ml-2 text-cyan-300">{audit.actor}</span>{/if}{#if audit.server_id}<span class="ml-2 text-slate-500">server #{audit.server_id}</span>{/if}<p class="mt-1 max-w-3xl truncate font-mono text-xs text-slate-500">{JSON.stringify(audit.details)}</p></div>
						<time class="text-xs text-slate-500" datetime={audit.created_at}>{new Date(audit.created_at).toLocaleString()}</time>
					</div>
				{:else}
					<div class="px-5 py-8 text-center text-sm text-slate-500">No audit events yet.</div>
				{/each}
			</div>
			<div class="flex items-center justify-between border-t border-slate-800 px-5 py-3 text-xs text-slate-500"><span>Offset {data.auditOffset}</span><div class="flex gap-2">{#if data.auditOffset > 0}<a class="rounded border border-slate-700 px-2 py-1 hover:border-cyan-400" href={`?event=${encodeURIComponent(data.auditEvent)}&offset=${Math.max(0, data.auditOffset - data.auditLimit)}`}>Previous</a>{/if}{#if data.auditHasNext}<a class="rounded border border-slate-700 px-2 py-1 hover:border-cyan-400" href={`?event=${encodeURIComponent(data.auditEvent)}&offset=${data.auditOffset + data.auditLimit}`}>Next</a>{/if}</div></div>
		</section>
	</div>
</main>
